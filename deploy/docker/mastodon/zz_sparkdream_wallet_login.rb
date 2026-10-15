# frozen_string_literal: true

# Wallet sign-in (SPARKDREAM_WALLET_LOGIN=true): members of the Spark Dream
# chains linked to this instance sign in through the sdaplogin sidecar, an
# OpenID Connect provider configured by the stock OIDC_* env. Mastodon
# approves accounts created through a provider even with registrations
# closed, so the provider, which checks x/rep membership on chain, is the
# only way new accounts appear. This file adds the pieces upstream lacks:
#
#   1. GET /sparkdream/login-chains.json: the chains whose members may sign
#      in, which the launcher stores here (mastodon-bootstrap login-chain
#      sync) and sdaplogin reads. Public chain info only.
#   2. The handle: upstream derives the username from the provider's uid.
#      That uid is the member's address, stable across chains and name
#      changes, so the handle comes from the provider's preferred_username
#      (the member's primary x/name) instead, with '-' as '_'. With no name
#      the account is not created.
#   3. GET /api/v1/sparkdream/wallet_addresses: for the bridge's follow-back
#      (sdapbridge followback.go), the wallet address of each given account
#      that signed in with a wallet and follows the calling bridge. Only a
#      bridge token may ask, so a member's wallet is shown to no one but the
#      bridge they chose to follow.
#   4. SparkdreamMembershipSweepWorker (hourly, see the image's sidekiq.yml
#      entry): disables the login of accounts whose owner is no longer a
#      member (asking sdaplogin at SPARKDREAM_LOGIN_URL/membership/<uid>),
#      and re-enables the ones it disabled once they are again.
#   5. Delegated sessions: a key holding an x/session SIGN_IN grant from a
#      member signs in to the member's account, and sdaplogin says so in the
#      sparkdream_delegate / _grant / _chain claims. The callback records the
#      session it opened against that grant, and
#      SparkdreamDelegatedSessionSweepWorker (every 10 minutes) ends the
#      session once sdaplogin reports the grant revoked, declined or expired
#      (SPARKDREAM_LOGIN_URL/sign-in-grant/<chain>/<id>). Ending it destroys
#      the session activation, which logs that browser out and revokes the
#      web app's token. Tokens of apps the delegate authorised from that
#      session belong to the account and stay until revoked in its settings.

module SparkdreamWalletLogin
  CHAINS_SETTING    = 'sparkdream_login_chains'
  DISABLED_SETTING  = 'sparkdream_login_disabled'
  DELEGATED_SETTING = 'sparkdream_delegated_sessions'
  PROVIDER          = 'openid_connect'

  def self.enabled?
    ENV['SPARKDREAM_WALLET_LOGIN'] == 'true'
  end

  # GETs path on sdaplogin and returns the parsed JSON body, or nil when it
  # could not answer. Net::HTTP, not Mastodon's Request, which refuses
  # private addresses (a test or an operator may point SPARKDREAM_LOGIN_URL
  # at the sidecar by service name; the launcher uses its public domain).
  def self.ask_login(path)
    base = ENV['SPARKDREAM_LOGIN_URL'].to_s.chomp('/')
    return nil if base.empty?

    uri = URI("#{base}#{path}")
    res = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == 'https', open_timeout: 5, read_timeout: 40) do |http|
      http.get(uri.request_uri)
    end
    return nil unless res.is_a?(Net::HTTPSuccess)

    JSON.parse(res.body)
  rescue StandardError => e
    Rails.logger.warn("sparkdream wallet login: #{path}: #{e.class}: #{e.message}")
    nil
  end

  # Sessions opened through a SIGN_IN grant, keyed by session activation id:
  # { "<id>" => { "user" => user id, "granter" => identity uid (hex),
  #   "grantee" => bech32, "chain" => sdaplogin chain key, "grant" => id } }.
  # Bounded by Mastodon's per-user session cap; the sweep drops entries whose
  # activation is gone.
  module DelegatedSessions
    def self.load
      raw = Setting[DELEGATED_SETTING]
      raw.is_a?(Hash) ? raw.stringify_keys : {}
    end

    # Re-reads before writing, so a sign-in and the sweep racing each other
    # each change only their own entries.
    def self.update
      sessions = load
      yield sessions
      Setting[DELEGATED_SETTING] = sessions
    end

    # Called after the OIDC callback signed user in: records the session when
    # the sign-in came through a grant, and forgets an earlier delegated
    # record for it when the owner signed in themselves.
    def self.capture(user, session_id, auth)
      return if user.nil? || !user.persisted? || session_id.blank?
      return unless auth&.provider.to_s == PROVIDER

      activation = user.session_activations.find_by(session_id: session_id)
      return if activation.nil?

      info = auth.extra&.raw_info || {}
      delegate = info['sparkdream_delegate'].to_s
      update do |sessions|
        if delegate.empty?
          sessions.delete(activation.id.to_s)
        else
          sessions[activation.id.to_s] = {
            'user' => user.id,
            'granter' => auth.uid.to_s,
            'grantee' => delegate,
            'chain' => info['sparkdream_chain'].to_s,
            'grant' => info['sparkdream_grant'].to_s,
          }
          Rails.logger.info("sparkdream wallet login: @#{user.account.username} signed in by #{delegate} through sign-in grant #{info['sparkdream_grant']}")
        end
      end
    rescue StandardError => e
      # never fail a sign-in over the bookkeeping; the session then lasts
      # until it is ended by hand
      Rails.logger.error("sparkdream wallet login: recording a delegated session: #{e.class}: #{e.message}")
    end
  end

  # Records each wallet sign-in's session (DelegatedSessions.capture).
  module CallbackRecorder
    def openid_connect
      super
      return unless SparkdreamWalletLogin.enabled?

      SparkdreamWalletLogin::DelegatedSessions.capture(
        @user, cookies.signed['_session_id'], request.env['omniauth.auth']
      )
    end
  end

  # The wallet addresses of the calling bridge's wallet-account followers.
  module WalletAddresses
    BRIDGE_APP = 'SparkDream bridge' # the app mastodon-bootstrap bridge-token makes
    MAX_IDS    = 100

    # a fresh hash per response: Rack middleware adds headers to it
    def self.json_headers
      { 'content-type' => 'application/json', 'cache-control' => 'no-store' }
    end

    def self.call(env)
      req    = Rack::Request.new(env)
      token  = req.get_header('HTTP_AUTHORIZATION').to_s[/\ABearer (.+)\z/, 1]
      access = token && Doorkeeper::AccessToken.by_token(token)
      unless access&.accessible? && access.application&.name == BRIDGE_APP
        return [401, json_headers, ['{"error":"a bridge token is required"}']]
      end

      bridge = User.find_by(id: access.resource_owner_id)&.account
      return [401, json_headers, ['{"error":"a bridge token is required"}']] if bridge.nil?

      ids       = Array(req.params['id']).first(MAX_IDS).map(&:to_i)
      followers = Follow.where(target_account_id: bridge.id, account_id: ids).pluck(:account_id)
      rows      = Identity.where(provider: PROVIDER).joins(:user)
                          .where(users: { account_id: followers })
                          .pluck('users.account_id', 'identities.uid')
      [200, json_headers, [rows.to_h { |account_id, uid| [account_id.to_s, uid] }.to_json]]
    end
  end

  # The handle for a new account from the provider's nickname.
  module Username
    private

    def user_params_from_auth(email, auth)
      params = super
      return params unless auth.provider.to_s == SparkdreamWalletLogin::PROVIDER

      handle = auth.info.nickname.to_s.downcase.tr('-', '_')
      # blank: save! fails on the username, which the callback controller
      # turns into its "could not create an account" notice
      params[:account_attributes][:username] = handle.blank? ? '' : ensure_unique_username(ensure_valid_username(handle))
      params
    end
  end
end

if SparkdreamWalletLogin.enabled?
  Rails.application.routes.prepend do
    get '/sparkdream/login-chains.json', to: lambda { |_env|
      [200, { 'content-type' => 'application/json', 'cache-control' => 'no-store' },
       [Setting[SparkdreamWalletLogin::CHAINS_SETTING].presence || '{}']]
    }
    get '/api/v1/sparkdream/wallet_addresses', to: SparkdreamWalletLogin::WalletAddresses
  end

  Rails.application.config.to_prepare do
    User.singleton_class.prepend(SparkdreamWalletLogin::Username) unless User.singleton_class.include?(SparkdreamWalletLogin::Username)
    callbacks = Auth::OmniauthCallbacksController
    callbacks.prepend(SparkdreamWalletLogin::CallbackRecorder) unless callbacks.include?(SparkdreamWalletLogin::CallbackRecorder)
  end
end

# Defined even when the feature is off, so a schedule entry never names a
# missing class; perform returns at once then.
class SparkdreamMembershipSweepWorker
  include Sidekiq::Worker

  sidekiq_options retry: 0, lock: :until_executed

  def perform
    return unless SparkdreamWalletLogin.enabled?
    return if ENV['SPARKDREAM_LOGIN_URL'].to_s.empty?

    disabled = Array(Setting[SparkdreamWalletLogin::DISABLED_SETTING]).map(&:to_i).to_set
    Identity.where(provider: SparkdreamWalletLogin::PROVIDER).includes(:user).find_each do |identity|
      user = identity.user
      next if user.nil?

      case membership(identity.uid)
      when 'inactive'
        # an account an admin already disabled stays theirs to manage
        next if user.disabled?

        user.disable!
        disabled << user.id
        Rails.logger.info("sparkdream wallet login: disabled @#{user.account.username}, no longer a member")
      when 'active'
        next unless disabled.include?(user.id)

        user.enable! if user.disabled?
        disabled.delete(user.id)
        Rails.logger.info("sparkdream wallet login: re-enabled @#{user.account.username}, a member again")
      end
    end
    Setting[SparkdreamWalletLogin::DISABLED_SETTING] = disabled.to_a
  end

  private

  # "active", "inactive", or nil when the provider could not say: nil
  # leaves the account as it is.
  def membership(uid)
    status = SparkdreamWalletLogin.ask_login("/membership/#{CGI.escape(uid.to_s)}")&.dig('status')
    %w(active inactive).include?(status) ? status : nil
  end
end

# Ends the sessions opened through a SIGN_IN grant once the grant is gone
# (point 5 above). Every 10 minutes, so a revoked grant stops working within
# minutes rather than within the hour.
class SparkdreamDelegatedSessionSweepWorker
  include Sidekiq::Worker

  sidekiq_options retry: 0, lock: :until_executed

  def perform
    return unless SparkdreamWalletLogin.enabled?
    return if ENV['SPARKDREAM_LOGIN_URL'].to_s.empty?

    ended = []
    SparkdreamWalletLogin::DelegatedSessions.load.each do |id, rec|
      activation = SessionActivation.find_by(id: id.to_i)
      if activation.nil? || activation.user_id != rec['user']
        ended << id # signed out, or purged as an old session
        next
      end
      next unless grant_status(rec) == 'inactive'

      username = activation.user&.account&.username
      activation.destroy # the browser is signed out on its next request
      ended << id
      Rails.logger.info("sparkdream wallet login: ended @#{username}'s session for #{rec['grantee']}, sign-in grant #{rec['grant']} is gone")
    end
    return if ended.empty?

    SparkdreamWalletLogin::DelegatedSessions.update { |sessions| ended.each { |id| sessions.delete(id) } }
  end

  private

  # "active", "inactive", or nil when sdaplogin could not say (the session
  # is left alone)
  def grant_status(rec)
    path = "/sign-in-grant/#{CGI.escape(rec['chain'].to_s)}/#{CGI.escape(rec['grant'].to_s)}" \
           "?granter=#{CGI.escape(rec['granter'].to_s)}&grantee=#{CGI.escape(rec['grantee'].to_s)}"
    status = SparkdreamWalletLogin.ask_login(path)&.dig('status')
    %w(active inactive).include?(status) ? status : nil
  end
end
