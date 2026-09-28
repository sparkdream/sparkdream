# frozen_string_literal: true

# Wallet sign-in (SPARKDREAM_WALLET_LOGIN=true): members of the Spark Dream
# chains linked to this instance sign in through the sdaplogin sidecar, an
# OpenID Connect provider configured by the stock OIDC_* env. Mastodon
# approves accounts created through a provider even with registrations
# closed, so the provider, which checks x/rep membership on chain, is the
# only way new accounts appear. This file adds the three pieces upstream
# lacks:
#
#   1. GET /sparkdream/login-chains.json: the chains whose members may sign
#      in, which the launcher stores here (mastodon-bootstrap login-chain
#      sync) and sdaplogin reads. Public chain info only.
#   2. The handle: upstream derives the username from the provider's uid.
#      That uid is the member's address, stable across chains and name
#      changes, so the handle comes from the provider's preferred_username
#      (the member's primary x/name) instead, with '-' as '_'. With no name
#      the account is not created.
#   3. SparkdreamMembershipSweepWorker (hourly, see the image's sidekiq.yml
#      entry): disables the login of accounts whose owner is no longer a
#      member (asking sdaplogin at SPARKDREAM_LOGIN_URL/membership/<uid>),
#      and re-enables the ones it disabled once they are again.

module SparkdreamWalletLogin
  CHAINS_SETTING   = 'sparkdream_login_chains'
  DISABLED_SETTING = 'sparkdream_login_disabled'
  PROVIDER         = 'openid_connect'

  def self.enabled?
    ENV['SPARKDREAM_WALLET_LOGIN'] == 'true'
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
  end

  Rails.application.config.to_prepare do
    User.singleton_class.prepend(SparkdreamWalletLogin::Username) unless User.singleton_class.include?(SparkdreamWalletLogin::Username)
  end
end

# Defined even when the feature is off, so a schedule entry never names a
# missing class; perform returns at once then.
class SparkdreamMembershipSweepWorker
  include Sidekiq::Worker

  sidekiq_options retry: 0, lock: :until_executed

  def perform
    return unless SparkdreamWalletLogin.enabled?

    base = ENV['SPARKDREAM_LOGIN_URL'].to_s.chomp('/')
    return if base.empty?

    disabled = Array(Setting[SparkdreamWalletLogin::DISABLED_SETTING]).map(&:to_i).to_set
    Identity.where(provider: SparkdreamWalletLogin::PROVIDER).includes(:user).find_each do |identity|
      user = identity.user
      next if user.nil?

      case membership(base, identity.uid)
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
  def membership(base, uid)
    uri = URI("#{base}/membership/#{CGI.escape(uid.to_s)}")
    # Net::HTTP, not Mastodon's Request, which refuses private addresses (a
    # test or an operator may point SPARKDREAM_LOGIN_URL at the sidecar by
    # service name; the launcher uses its public domain)
    res = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == 'https', open_timeout: 5, read_timeout: 40) do |http|
      http.get(uri.request_uri)
    end
    return nil unless res.is_a?(Net::HTTPSuccess)

    status = JSON.parse(res.body)['status']
    %w(active inactive).include?(status) ? status : nil
  rescue StandardError => e
    Rails.logger.warn("sparkdream wallet login: membership of #{uid}: #{e.class}: #{e.message}")
    nil
  end
end
