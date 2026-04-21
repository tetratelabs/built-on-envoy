# Releasing Built On Envoy

Built On Envoy consists of two main artifact types: the CLI and the different extensions. The release cycle of these
is independent and can be released at different times based on needs.

## Releasing the CLI

The CLI process is automatically triggered when a Git release tag is created. Once the tag is pushed, the release workflow
will start and:

* Build and push the CLI Docker images to the BOE image registry.
* Generate the draft release notes page in the [release list](https://github.com/tetratelabs/built-on-envoy/releases).
* Attach the CLI binaries to the release notes.

Once the workflow completes, you need to review and update the generated draft release notes and make them public.

### Updating the Homebrew formula

There is a Homebrew formula for the CLI at: https://github.com/tetratelabs/homebrew-boe

The formula can be automatically updated by calling the [Update Formula](https://github.com/tetratelabs/homebrew-boe/actions/workflows/update-formula.yaml)
workflow. It can be triggered directly from the GitHub website as a `workflow_dispatch`, passing the BOE CLI version as an
argument. The workflow will automatically update the formula code and SHAs to point to the given BOE CLI release.

## Releasing extensions

Extensions are released automatically whenever changes are done, so there is no need, in general, to manually release the extensions.

The Go-based extensions are an exception, as they are all released together in the `composer` dynamic module and use the `-dev` suffix in the
version during development. To cut a release of teh `composer` dynamic module:

* Update the version in the [composer manifest](https://github.com/tetratelabs/built-on-envoy/blob/main/extensions/composer/manifest.yaml) and
  remove the `-dev` suffix.
* Run `make -C cli gen` to regenerate the extension manifest index for the website.
* Open a pull request with the changes.
* Once merged, do the same to bump the version of `composer` to the next `-dev` version.

### Manually releasing extensions

There are also several [GitHub Actions workflows](https://github.com/tetratelabs/built-on-envoy/actions) to manually release each extension. Those
can be run directly from the GitHub website as a `workflow_dispatch`.
