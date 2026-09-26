# frozen_string_literal: true

# Deployed by the SparkDream launcher, TLS ends before a request reaches puma:
# at Cloudflare and/or the Akash provider's ingress, which then speaks plain
# HTTP to the pod and rewrites X-Forwarded-Proto to "http". Stock production
# config has force_ssl on and assume_ssl off, so every request would be
# redirected to https and arrive as http again -- a redirect loop. With
# SPARKDREAM_ASSUME_SSL=true Rails treats every request as HTTPS (secure
# cookies, https URLs, no redirect), which is true for everything the ingress
# forwards. Only set it where the public endpoint really is HTTPS.
Rails.application.config.assume_ssl = true if ENV['SPARKDREAM_ASSUME_SSL'] == 'true'
