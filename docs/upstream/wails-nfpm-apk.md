# Wails does not expose its registered nFPM APK packager

- **Target repository:** `wailsapp/wails`
- **Kind:** bug report or small enhancement
- **Confidence:** High
- **Status:** Draft; not filed. Recheck the latest Wails v3 release first.

## Summary

Wails v3.0.0-alpha2.121 registers nFPM's Alpine APK packager and defines an
internal `APK` package type, but `wails3 tool package` rejects `-format apk`.
The command accepts only `deb`, `rpm`, `archlinux`, and `dmg`.

## Reproduction

Given a valid nFPM configuration:

```sh
wails3 tool package \
  -name PSNWDL \
  -format apk \
  -config ./build/linux/nfpm/psnwdl.yaml \
  -out ./bin
```

Wails returns:

```text
unsupported package format 'apk'. Supported formats: deb, rpm, archlinux, dmg
```

In the same Wails release, `internal/packager/packager.go` blank-imports
`github.com/goreleaser/nfpm/v2/apk`, declares `APK PackageType = "apk"`, and
can pass a selected package type to nFPM. The omission is in the command's
format-selection switch and help text.

## Downstream workaround in PSNWDL

PSNWDL still uses Wails to build the application natively inside an Alpine
3.22 container so the binary targets musl. Its `generate:apk` task then invokes
nFPM v2.47.0 directly with the same package configuration used by Wails for
DEB, RPM, and Arch packages.

The workaround should stay until the pinned Wails command accepts APK. Merely
wrapping the Ubuntu/glibc binary in an APK is not a valid substitute for the
native Alpine build.

## Could this resolve without filing?

Yes. Wails v3 is prerelease software, and the internal APK registration suggests
the public switch may be completed in a later release. Before filing, install
the newest Wails v3 CLI and rerun the reproduction. If it succeeds, record the
fixing version in `docs/upstream.md` and replace the direct nFPM command.

## The ask

Add `apk` to the `wails3 tool package` format switch and help text, mapping it
to the existing internal `packager.APK` type, with a command-level test.
