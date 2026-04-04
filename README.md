# tools

## Bazel

This repository supports Bazel builds in parallel with the existing Go tooling.

Common entrypoints:

- `bazel test //...`
- `bazel build //...`

Cross-platform builds:

- `bazel build --config=linux_amd64 //...`
- `bazel build --config=darwin_amd64 //...`
- `bazel build --config=darwin_arm64 //...`

## XR Proto Helper

This repository also includes an IOS XR `GetProtoFile` helper:

- target: `//xr_getproto:xr_getproto`
- docs: [`xr_getproto/README.md`](/Users/sbezverk/projects/go/workspace/tools/xr_getproto/README.md)
