# Chocolatey publication

PSNWDL 0.1.1 was approved by moderator virtualex on October 15, 2026.
[Package](https://community.chocolatey.org/packages/psnwdl/0.1.1).

The moderator verified both ZIPs against the GitHub release digests and accepted
the WebView2 dependency. They requested a copyright element matching LICENSE and
a fuller description for the next release, explicitly saying not to resubmit
0.1.1. The generator now derives copyright from LICENSE and uses the fuller
chocolateyDescription in product.json. These changes apply to future packaging;
the approved public package is unchanged and no new app release is needed.

Automated validator suggestions also included an icon, more documentation/source
links and a minimum dependency version. These did not block approval. A minimum
WebView2 version should reflect an established runtime requirement rather than an
arbitrary current version. Normal stable-release automation remains configured.

## Icon update

The publisher requested icons for the current listings on October 20. Approved
0.1.1 cannot be overwritten; prepare Chocolatey-only revision 0.1.1.20261020
using the unchanged 0.1.1 application assets. This also carries the already
prepared copyright and fuller description. WinEnvEdit is still under review and
can retain its existing Chocolatey version. The distribution workflow's optional
chocolatey_version input affects only the nuspec, not app VERSION, release URLs,
checksums, or Scoop version. Icon URLs use commit-pinned jsDelivr PNGs verified
against the repository images.

## Scan note

The October 13 VirusTotal report for the official x64 ZIP (SHA256
cb62096274e20c6397e56be4974f6e925d8dbc77a8b517745903dee4f9675acb)
shows Bkav Pro as the only detecting engine, label W32.Malware.B482907F (1/68).
The others report undetected. The label alone does not establish a cause or
prove a false positive. The ZIP matches the published GitHub asset digest.
Chocolatey's moderator approved the original package with this scan note.
[Report](https://www.virustotal.com/gui/file/cb62096274e20c6397e56be4974f6e925d8dbc77a8b517745903dee4f9675acb/detection).
