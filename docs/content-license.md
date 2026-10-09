# Open Content: Everything on Spark Dream Is Public Domain

**Everything published on Spark Dream is dedicated to the public domain under [CC0 1.0 Universal](https://creativecommons.org/publicdomain/zero/1.0/).** Anyone may copy, modify, translate, remix, sell, archive or build on any of it, for any purpose, without asking permission and without paying anyone.

That is not a setting. It is the chain's founding commitment, compiled into the binary, and governance cannot change it.

## Why

Spark Dream is a commons. Members earn reputation by producing things the community values, and the chain pays for public goods. Content that someone has to license back from its author is not a commons, it is a set of private holdings stored in a public place. So the chain takes the simplest position available:

- **No permission layer.** Nobody needs to identify an author, negotiate terms or track attribution chains before reusing what is here. A post can be quoted in a book, a forum thread can train a translator, a collection can be forked into another chain's archive.
- **Nothing to relicense later.** A chain that collects content under one license is one governance vote away from changing it. Public-domain content cannot be enclosed again: once dedicated, it stays free.
- **Federation without friction.** Sister chains and bridged networks exchange content under one rule everyone already knows, instead of reconciling license terms per peer.
- **Honest about permanence.** Anything written to a public chain is copied to every node and every archive that follows it. CC0 says plainly what that already means in practice.

Attribution is still good manners. The chain records who wrote what, and clients show it. CC0 just means credit is a courtesy, not a legal condition.

## What participants agree to

Members sign the agreement when they join: accepting an invitation (`MsgAcceptInvitation`) requires `accepted_content_license = "CC0-1.0"`, and the chain refuses the acceptance otherwise. For everyone else, including non-members posting ephemeral content, submitting content is the dedication. The statement clients show, and that the chain returns from its `ContentLicense` query, is:

> Everything published on Spark Dream is dedicated to the public domain under CC0 1.0 Universal. By submitting content you waive all copyright and related rights in it, worldwide, to the extent the law allows, so that anyone may copy, modify, distribute and build on it, for any purpose, without asking permission. Submit only work you have the right to dedicate this way.

In practice:

- **Post only what you have the right to give away.** Your own words, images and code are fine. Someone else's copyrighted work is not yours to dedicate, even with credit. If it has to be there, link to it rather than copying it in.
- **It covers everything you publish here.** Blog posts and replies, forum threads and replies, collections and their items, comments, profiles and display names, tag descriptions, proposals and initiative write-ups. Anonymous content published through x/shield is included: the dedication does not depend on your name being on it.
- **A reference is not a copy.** A collection item that points at an NFT, a web page or another post dedicates what you wrote about it (its title, description, tags, your curation), not the work it points at.
- **It cannot be withdrawn.** CC0 is irrevocable, and so is a committed block. Editing or deleting a post on Spark Dream does not take back earlier versions, which stay public domain.
- **Moderation is separate.** Sentinels and councils can hide content that breaks community rules, including content posted without the right to dedicate it. Hiding a post does not change its license.

Code delivered through x/reveal is not stored on-chain. Once fully revealed it is released under the proposal's `final_license`, which the chain requires to be public domain or a permissive open-source license: `CC0-1.0`, `Unlicense`, `0BSD`, `MIT`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause` or `ISC`. Copyleft licenses are refused, since their share-alike terms are conditions on reuse. The license during the reveal (`initial_license`) is the contributor's choice; the code is not yet public then.

## Content from elsewhere

The same rule holds at the chain's borders. x/federation admits inbound content only when it is already public domain, and every federated record carries the license it entered under:

| License | Meaning | How it is shown on Mastodon |
|---------|---------|-----------------------------|
| `CC0-1.0` | The author waived all rights | `#cc0` in the post |
| `PDM-1.0` | The work is already free of known copyright ([Public Domain Mark](https://creativecommons.org/publicdomain/mark/1.0/)) | `#publicdomain` in the post |

- **Mastodon and other ActivityPub servers.** The bridge anchors a post only when its author tagged it `#cc0` or `#publicdomain`, on top of the author's opt-in (following the bridge account). The chain refuses any submission without one of the two licenses (`ErrLicenseNotAccepted`, code 2389). An independent verifier re-fetches every post and refuses to vouch for one whose hashtag does not match the license claimed for it. The hashtag sits in the hashed post body, so it cannot be removed after verification without the change showing.
- **Sister Spark Dream chains.** Content sent over IBC carries `CC0-1.0`. A receiving chain refuses a packet without a public-domain license.
- **Content leaving the chain** is already CC0, so peers receive it with no conditions.

Details: [x/federation spec](x-federation-spec.md), Sections 3.3 and 6.15.

## Where the license lives

| Surface | What it shows |
|---------|---------------|
| `sparkdreamd query sparkdream content-license` | License id, name, legal-text URL, the dedication statement, accepted federation licenses |
| `GET /sparkdream/sparkdream/v1/content_license` | Same, over REST |
| [x/common/types/content_license.go](../x/common/types/content_license.go) | The constants every module and daemon uses |
| `FederatedContent.license` | The license each federated record entered under |
| `MsgAcceptInvitation.accepted_content_license` | Each member's signed agreement; event `content_license_accepted` |
| `Contribution.final_license` (x/reveal) | The open license revealed code is released under |
| The Mastodon bridge account's bio | The opt-in rule (`#cc0` / `#publicdomain`), set when the account is provisioned |

## For client developers

- **Show the dedication wherever content is written.** In a composer, next to the submit button: "Published to the public domain (CC0)", with a link to the full statement. Read the text from the `ContentLicense` query rather than hard-coding it.
- **Say it before membership.** Invitation acceptance shows the commitment in full and sets `accepted_content_license` only after the invitee explicitly agrees.
- **Label federated content** with its `license`, and keep the remote author's attribution visible.
- **Show the license in exports and feeds** (RSS, ActivityPub output, data dumps) as CC0 1.0, so that reusers can see the content is free without reading this document.

## Why a constant, not a parameter

Governance controls almost everything on Spark Dream, but not the content license. Content already published was contributed on the promise that it would stay free. A vote that narrowed the license would put new terms on future contributions and leave the old ones ambiguous, and a vote that widened it would break the promise in the other direction. Fixing the license in code means a change takes a chain upgrade that every validator has to adopt, the same bar as the inflation bounds ([security-hardening.md](security-hardening.md)).

*This page describes the chain's policy. It is not legal advice; the [CC0 legal code](https://creativecommons.org/publicdomain/zero/1.0/legalcode) is the authoritative text.*
