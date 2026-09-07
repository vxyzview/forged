# forged on any CI/CD system

`forged build --ci` works on **every** CI/CD platform. forged auto-detects
where it runs, never prompts for input, and speaks each provider's native
log dialect — collapsible sections, error annotations, machine-readable
outputs and artifacts.

## Quick reference

| Provider | Detection (env var) | Log groups | Annotations | Extra outputs |
|---|---|---|---|---|
| GitHub Actions | `GITHUB_ACTIONS=true` | `::group::` | `::error` / `::warning` | `$GITHUB_OUTPUT`, `$GITHUB_STEP_SUMMARY` |
| GitLab CI | `GITLAB_CI` | `section_start`/`section_end` | job log | — |
| Azure Pipelines | `TF_BUILD=true` | `##[group]` | `##vso[task.logissue]` | — |
| TeamCity | `TEAMCITY_VERSION` | `blockOpened`/`blockClosed` | `##teamcity[message]` | — |
| Buildkite | `BUILDKITE=true` | `---` folds | job log | — |
| Jenkins | `JENKINS_URL` | plain banners | job log | — |
| CircleCI | `CIRCLECI=true` | plain banners | job log | — |
| Travis CI | `TRAVIS=true` | plain banners | job log | — |
| Drone | `DRONE=true` | plain banners | job log | — |
| Bitbucket Pipelines | `BITBUCKET_BUILD_NUMBER` | plain banners | job log | — |
| AppVeyor | `APPVEYOR=true` | plain banners | job log | — |
| Woodpecker | `WOODPECKER=true` | plain banners | job log | — |
| Cirrus CI | `CIRRUS_CI=true` | plain banners | job log | — |
| Generic (`CI=true`) | `CI=true` | plain banners | job log | — |
| Anywhere (forced) | `FORGED_CI=1` | plain banners | job log | — |

## Universal flags

```bash
forged build --ci \
  --source . \
  --defconfig vendor/your_device_defconfig \
  --arch arm64 \
  --toolchain-dir "$HOME/.cache/forged/toolchains" \
  --version-tag "$(git rev-parse --short HEAD)"
```

- `--ci` — no interactive prompts (toolchain problems fail the build), no
  TUI, provider-native log groups + annotations + report. Auto-enabled when
  any known CI environment is detected, so most pipelines don't need it.
- `FORGED_CI=1` — force CI mode when your system sets no CI env vars
  (cron, systemd timers, self-hosted scripts).
- `FORGED_CI_NO_PROMPT=1` — make `git clone` fail fast instead of prompting
  for credentials (already automatic in all detected CIs).
- `--toolchain-dir` — point at a cacheable path so AOSP Clang survives
  between builds.
- `--toolchain-extra-path DIR` — use a pre-provisioned clang, skip download.

Every build also writes an errors + warnings digest log (see
`forged build --help`, `--log-file`) — attach it as an artifact alongside
the flashable ZIP in `releases/` or `out/logs/`.

## GitHub Actions

Copy [`.github/workflows/build-kernel.yml`](../.github/workflows/build-kernel.yml)
into your kernel repo — it handles host deps, forged install, toolchain +
ccache caching, ZIP artifact upload and release-on-tag. Outputs from the
build step: `outcome`, `zip_path`, `zip_name`, `log_path`.

## GitLab CI

```yaml
build-kernel:
  stage: build
  image: ubuntu:24.04
  variables:
    GIT_SUBMODULE_STRATEGY: none
  before_script:
    - apt-get update -qq && apt-get install -y -qq bc bison build-essential
      ccache flex libelf-dev libssl-dev llvm lld aria2
      gcc-aarch64-linux-gnu gcc-arm-linux-gnueabi curl
    - curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
    - forged setup-toolchain --preset aosp-clang --install-cross-compilers
  script:
    # GITLAB_CI is set: forged emits collapsible sections automatically.
    - forged build --source "$CI_PROJECT_DIR" --defconfig "$DEFCONFIG"
  cache:
    key: aosp-clang-r584948b
    paths:
      - ~/.local/share/forged/toolchains/
      - .ccache/
  artifacts:
    paths:
      - releases/*.zip
    when: always
```

## Jenkins (Declarative Pipeline)

```groovy
pipeline {
  agent { label 'linux && x86_64' }   // AOSP Clang prebuilts are x86_64-only
  environment {
    PATH = "${HOME}/.local/bin:${PATH}"
  }
  stages {
    stage('setup') {
      steps {
        sh 'curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash'
        sh 'forged setup-toolchain --preset aosp-clang --install-cross-compilers'
      }
    }
    stage('build') {
      steps {
        // JENKINS_URL is detected: no prompts, plain log banners.
        sh 'forged build --source "." --defconfig "vendor/your_device_defconfig"'
      }
    }
  }
  post {
    always {
      archiveArtifacts artifacts: 'releases/*.zip, out/logs/*.log', allowEmptyArchive: true
    }
  }
}
```

## Azure Pipelines

```yaml
pool:
  vmImage: ubuntu-latest        # TF_BUILD=true is set automatically
steps:
  - script: |
      curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
      forged setup-toolchain --preset aosp-clang --install-cross-compilers
    displayName: install forged + toolchain
  - script: |
      forged build --source "$(Build.SourcesDirectory)" --defconfig "vendor/your_device_defconfig"
    displayName: build kernel
  - task: PublishPipelineArtifact@1
    inputs:
      targetPath: releases
      artifactName: kernel-zips
```

## CircleCI

```yaml
jobs:
  build-kernel:
    machine:
      image: ubuntu-2404:current   # CIRCLECI=true is set automatically
    steps:
      - checkout
      - run:
          name: install forged + toolchain
          command: |
            curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
            forged setup-toolchain --preset aosp-clang --install-cross-compilers
      - run:
          name: build kernel
          command: forged build --source . --defconfig "vendor/your_device_defconfig"
      - store_artifacts:
          path: releases/
```

## Anything else

If your CI exports nothing forged recognizes, set `FORGED_CI=1` in the
environment — the build behaves identically (no prompts, plain banners,
report in the log). Everything works on self-hosted runners too.

## Caching rules of thumb

- Cache the toolchain dir (`~/.local/share/forged/toolchains`, keyed on the
  clang version) — a ~1 GB download saved per build.
- Cache `ccache` (`~/.cache/ccache` or per-project `.ccache`) and enable it
  with `--ccache`; rebuilds drop from ~45 min to minutes.
- Never cache `out/` from the kernel tree itself.
