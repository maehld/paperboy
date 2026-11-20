# 0141: Keep CalDAV credentials on their hosts

- Status: Accepted
- Datum: 2026-11-20

## Kontext

The calendar plugin follows redirects by hand and sets the Basic credentials
again on every request. It has to: iCloud answers on `caldav.icloud.com` and
moves every account to its own `pNN-caldav.icloud.com` host, and the plugin must
not lose the app password there. But any configured or compromised server can
redirect to another host, and the plugin then hands it the app password.

## Entscheidung

The client keeps the credentials for a redirect to the same host and for a
redirect between two iCloud hosts. Any other host change drops them for the rest
of the chain. A userinfo part of the redirect URL is removed too, because the
HTTP client would turn it into an Authorization header.

The comparison uses the host name and ignores the port. `icloud.com` and every
name below it count as iCloud.

## Konsequenzen

iCloud keeps working, including the move to a partition host. A server that
redirects to a second host loses the credentials and answers `401`, which the
plugin reports as a failed collection. The rule names iCloud explicitly; a
provider that moves accounts to a host outside `icloud.com` needs its own
allowlist entry.
