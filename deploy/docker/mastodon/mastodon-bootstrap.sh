#!/bin/bash
# ------------------------------------------------------------------
# One-shot instance setup the launcher runs over lease-shell (the image
# runs no sshd). Every action is idempotent and prints one JSON object on
# stdout as its last line; progress goes to stderr.
#
#   mastodon-bootstrap owner <username> <email>
#       create the Owner account if missing (confirmed + approved)
#       -> {"created":true,"password":"..."} | {"created":false}
#   mastodon-bootstrap registrations <open|approved|none>
#       who may sign up: anyone, anyone with approval, or nobody
#       -> {"registrations":"approved"}
#   mastodon-bootstrap bridge-token <username> <email>
#       the account the ActivityPub bridge reads through (authors opt in by
#       following it), and an API token for it (read + write:follows: it
#       follows the authors its peers' curation admits); the same token on
#       every call, replacing an older read-only one. Also (re)writes the
#       account's bio and profile fields with the open-content rule, so
#       every author who considers following it reads that only #cc0 /
#       #publicdomain posts are relayed and that they become public domain
#       -> {"token":"..."}
#   mastodon-bootstrap login-chain sync '<json object>'
#       wallet sign-in: replace the chains whose members may sign in (keyed
#       by chain fleet id; served at /sparkdream/login-chains.json for the
#       sdaplogin sidecar). {} turns sign-in off for every chain
#       -> {"chains":2}
# ------------------------------------------------------------------
set -euo pipefail
cd /opt/mastodon
as_mastodon() { setpriv --reuid=991 --regid=991 --init-groups env HOME=/opt/mastodon "$@"; }

account_exists() {  # <username>
    as_mastodon bundle exec rails runner "exit(Account.find_local('$1') ? 0 : 1)" >/dev/null 2>&1
}

# Create a confirmed, approved local account and print its generated
# password. Done in Ruby rather than `tootctl accounts create`, which runs
# Mastodon's email MX check: an instance's own fresh domain (the bridge
# account's address) usually has no MX records, and production has no switch
# to skip the check. It is skipped here for this record only -- the accounts
# the launcher creates are its own, never self-service sign-ups.
create_account() {  # <username> <email> [role name]
    as_mastodon bundle exec rails runner "
        password = SecureRandom.hex(16)
        role = '${3:-}'.empty? ? nil : UserRole.find_by!(name: '${3:-}')
        user = User.new(email: '$2', password: password, agreement: true, approved: true,
                        confirmed_at: Time.now.utc, role: role, bypass_registration_checks: true)
        user.define_singleton_method(:validate_email_dns?) { false }
        user.account = Account.new(username: '$1')
        user.save!
        # create resets approval from the registrations mode; an unapproved
        # account answers 404 on its profile and AS2 actor
        user.approve!
        puts password
    " | tail -1
}

# The bridge account's profile: the chain accepts only public-domain
# content (docs/content-license.md), so an author must know before opting in
# that following means "my #cc0 posts are relayed, and nothing else". Plain
# text; Mastodon links the hashtags. Bio limit 500 characters.
BRIDGE_NOTE="I relay posts to the Spark Dream chain, where everything is public domain. \
To take part, follow me, then tag a post #cc0 to dedicate your own work under CC0 1.0, \
or #publicdomain for a work already free of copyright. Untagged posts are never relayed. \
Relayed posts stay public domain for good; unfollow at any time to stop."
BRIDGE_FIELDS='[
  {"name":"License","value":"CC0 1.0 / public domain only"},
  {"name":"Opt in","value":"Follow, then tag posts #cc0 or #publicdomain"},
  {"name":"CC0","value":"https://creativecommons.org/publicdomain/zero/1.0/"}
]'

case "${1:-}" in
    owner)
        user=$2; email=$3
        if account_exists "$user"; then
            echo '{"created":false}'
        else
            pw=$(create_account "$user" "$email" Owner)
            [ -n "$pw" ] || { echo "account creation printed no password" >&2; exit 1; }
            printf '{"created":true,"password":"%s"}\n' "$pw"
        fi
        ;;
    registrations)
        mode=$2
        case "$mode" in open|approved|none) ;; *) echo "mode must be open, approved or none" >&2; exit 2 ;; esac
        as_mastodon bundle exec rails runner "Setting.registrations_mode = '$mode'" >&2
        printf '{"registrations":"%s"}\n' "$mode"
        ;;
    bridge-token)
        user=$2; email=$3
        account_exists "$user" || create_account "$user" "$email" >/dev/null
        # profile through the environment, not the Ruby source: it is data
        BRIDGE_NOTE="$BRIDGE_NOTE" BRIDGE_FIELDS="$BRIDGE_FIELDS" as_mastodon bundle exec rails runner "
            account = Account.find_local('$user')
            fields = JSON.parse(ENV.fetch('BRIDGE_FIELDS'))
                         .each_with_index.to_h { |f, i| [i.to_s, f.with_indifferent_access] }
            account.note = ENV.fetch('BRIDGE_NOTE')
            account.fields_attributes = fields
            if account.changed?
              account.save!
              # followers' instances refresh the profile
              ActivityPub::UpdateDistributionWorker.perform_async(account.id)
            end
            user = account.user
            # read + write:follows: the bridge reads its home timeline and
            # follows exactly the authors its peers' curation admits
            scopes = 'read write:follows'
            app = Doorkeeper::Application.find_or_create_by!(name: 'SparkDream bridge') do |a|
              a.redirect_uri = 'urn:ietf:wg:oauth:2.0:oob'
              a.scopes = scopes
            end
            app.update!(scopes: scopes) unless app.scopes.to_s == scopes
            live = Doorkeeper::AccessToken.where(application: app, resource_owner_id: user.id, revoked_at: nil)
            tok = live.find { |t| t.scopes.to_s.split.include?('write:follows') }
            unless tok
              # an older read-only token: replace it (the launcher re-delivers)
              live.each(&:revoke)
              tok = Doorkeeper::AccessToken.create!(application: app, resource_owner_id: user.id, scopes: scopes)
            end
            puts({ token: tok.token }.to_json)
        " | tail -1
        ;;
    login-chain)
        [ "${2:-}" = "sync" ] || { echo "usage: mastodon-bootstrap login-chain sync '<json object>'" >&2; exit 2; }
        # through the environment, not the Ruby source: the JSON is data
        SPARKDREAM_LOGIN_CHAINS="${3:-}" as_mastodon bundle exec rails runner '
            chains = JSON.parse(ENV.fetch("SPARKDREAM_LOGIN_CHAINS"))
            abort "login-chain sync: expected a JSON object" unless chains.is_a?(Hash)
            Setting["sparkdream_login_chains"] = chains.to_json
            puts({ chains: chains.size }.to_json)
        ' | tail -1
        ;;
    *)
        echo "usage: mastodon-bootstrap owner|registrations|bridge-token|login-chain ..." >&2
        exit 2
        ;;
esac
