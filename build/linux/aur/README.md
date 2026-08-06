# AUR package

Each GitHub release contains a `PSNWDL-<version>-linux-aur-source.tar.gz` source
package. It contains the versioned `PKGBUILD` and `.SRCINFO` for the
`psnwdl-bin` AUR package. The recipe downloads the matching prebuilt
`linux-x86_64` or `linux-aarch64` tarball from GitHub Releases and declares
`gtk4` and `webkitgtk-6.0` as runtime dependencies.

Publishing to the AUR requires a separate AUR account and SSH repository. After
a GitHub release is available, extract its AUR source package, review it, and
commit `PKGBUILD` plus `.SRCINFO` to:

```text
ssh://aur@aur.archlinux.org/psnwdl-bin.git
```

The release workflow generates the architecture-specific checksums; do not use
`SKIP` for the remote binary sources.
