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
#       following it), and a read-only API token for it; the same token on
#       every call
#       -> {"token":"..."}
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
        as_mastodon bundle exec rails runner "
            user = Account.find_local('$user').user
            app = Doorkeeper::Application.find_or_create_by!(name: 'SparkDream bridge') do |a|
              a.redirect_uri = 'urn:ietf:wg:oauth:2.0:oob'
              a.scopes = 'read'
            end
            tok = Doorkeeper::AccessToken.where(application: app, resource_owner_id: user.id, revoked_at: nil).first ||
                  Doorkeeper::AccessToken.create!(application: app, resource_owner_id: user.id, scopes: 'read')
            puts({ token: tok.token }.to_json)
        " | tail -1
        ;;
    *)
        echo "usage: mastodon-bootstrap owner|registrations|bridge-token ..." >&2
        exit 2
        ;;
esac
