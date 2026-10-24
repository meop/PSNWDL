# Distribution packages

Run from the repository root with PowerShell 7 and authenticated GitHub CLI:

    ./scripts/New-DistributionPackages.ps1 -Version RELEASE_VERSION

Use the existing published version; no version bump or new app release is needed.
The generator verifies release asset sizes, available GitHub SHA256 digests and
portable ZIP layout. Output is ignored under out/distribution. Optional
AssetsDirectory and OutputDirectory parameters support a local cache and staging.

Scoop: both projects are published in the shared
[meop bucket](https://github.com/meop/scoop-bucket). Add it with
`scoop bucket add meop https://github.com/meop/scoop-bucket`, then install with
`scoop install meop/psnwdl`. Its independent updater verifies both architectures'
release assets on manual refresh. The app's stable-release pipeline calls its
reusable publisher to update this app immediately after release. The configured
SCOOP_BUCKET_SSH_KEY secret has write access only to the bucket; no personal token
is needed. The distribution workflow can also publish an existing release manually.
The generated manifest here remains a reviewable artifact.
New PSNWDL releases use `psnwdl-{version}-windows-{arch}.zip`; both generators
also accept the original `-portable.zip` names when refreshing older releases.
Scoop autoupdate points to the new naming scheme for subsequent releases.
Public Games/Extras inclusion is deferred until established usage or an accepted
exception. Keep the same package names during any migration.

Chocolatey: pack the generated nuspec with choco pack. The workflow packs it and
uploads reviewable artifacts. Publication stays disabled until initial install,
upgrade and uninstall smoke checks pass and a maintainer account is available.
Set repository variable CHOCOLATEY_PUBLISH_ENABLED to true and configure
CHOCOLATEY_API_KEY in the chocolatey environment to enable submission. Configure
environment protection if publication should require review. Chocolatey moderation
remains external. Never put credentials in package files or logs.

The reusable distribution workflow runs after successful stable releases, and
workflow_dispatch can package an already published release. Calling it directly
from the release pipeline handles releases created with GitHub's default token,
which do not trigger a second release-event workflow. WinGet remains separate.

Validation does not install packages or change environment variables. Native
smoke tests remain a publication gate; artifact creation is not a public listing.

## Channels and deferrals

PSNWDL follows RPCS3's actual distribution footprint. A channel RPCS3 does not
use is not pursued without a deliberate scope change.

| Channel | State | Reasoning |
|---|---|---|
| GitHub releases | Published | Windows, Linux and macOS, x64 and ARM64 |
| Scoop | Published | Shared meop bucket; public Games inclusion deferred until established usage or an accepted exception |
| Chocolatey | Published | Approved October 15, 2026. The moderator asked for copyright and a fuller description on the next package only |
| AUR `psnwdl-bin`, `psnwdl-git` | Blocked | Recipe generator exists, but registration was unavailable, so there is no maintainer account |
| WinGet | Deferred | RPCS3's own inclusion request is still open |
| Microsoft Store | Deferred | No official RPCS3 listing to follow |
| Homebrew | Deferred | RPCS3's cask submission failed signature verification, and releases here use ad-hoc macOS signing |
| Flathub, Nixpkgs, Snap, Void | Deferred | Separate packaging and ongoing maintenance burden |
| FreeBSD | Deferred | Would need a supported build or port first |

## Untested coverage

Published is not the same as exercised. These gaps are known and open:

- ARM64 native runtime, on every channel. Package contents and asset hashes are
  verified; launching the ARM64 binary from an installed package is not.
- True upgrades between different application versions. Only forced same-version
  reinstalls have been tested.
- Installation on a clean machine without prerequisites. The Chocolatey smoke
  test passed `--ignore-dependencies` because the host already had WebView2, so
  installing `webview2-runtime` as a declared dependency has never run.

## TODO: ARM64 and clean-machine runtime checks

Open, needs hardware that is not the x64 development box.

- **ARM64 native runtime**, per channel. Package contents and asset hashes are
  verified by the generators; nothing has launched an ARM64 binary from an
  installed package. Install from the meop Scoop bucket and from the Chocolatey
  package on ARM64, launch, and confirm a responding window.
- **WebView2 dependency install.** The Chocolatey smoke test passed
  `--ignore-dependencies` because the host already had WebView2, so installing
  `webview2-runtime` as a declared dependency has never run. Needs a machine
  without it, or a fresh VM.
- **True cross-version upgrades.** Only forced same-version reinstalls have been
  tested. Not actionable until a second release exists.

Record results per channel. A non-interactive session result is neither a pass
nor a failure of the application: GUI apps may fail to initialize there for
reasons unrelated to the build, so launch from a desktop session.

### State of the ARM64 machine, checked October 24, 2026

Useful for planning, so a trip there is not wasted:

- PSNWDL is **not installed** and the repo is **not cloned**; clone it if you
  want to build rather than install a published package.
- Scoop is present with only the `main` and `nerd-fonts` buckets, so the meop
  bucket is **not** added — a genuinely clean install test is available:
  `scoop bucket add meop https://github.com/meop/scoop-bucket` then
  `scoop install meop/psnwdl`, launch, confirm a responding window, then
  `scoop uninstall psnwdl` and remove the bucket to restore the machine.
- WebView2 runtime **is already installed** (156.0.4314.42), so the
  `webview2-runtime` dependency install **cannot** be exercised there either.
  That one still needs a fresh VM or a machine without Edge WebView2.
