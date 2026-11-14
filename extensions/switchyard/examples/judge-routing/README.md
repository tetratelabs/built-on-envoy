# Switchyard with two judges

This demo runs the Switchyard extension in a local kind cluster with Envoy Gateway and Agent
Router, and compares two judges that decide which real model serves each coding-agent request.

Two virtual models route the same prompts to the same two target models, an efficient one and a
capable one. Both use Switchyard's `llm_classifier` algorithm in capability mode, with a different
judge:

- `coding-jev` asks [Jev](https://typesafe.ai), a decision judge, whether the capable model has a
  real advantage on the task. If the advantage score is above the 0.15 cutoff, the request goes to
  the capable model; otherwise it goes to the efficient one.
- `coding-classifier` asks an LLM, through an OpenAI-compatible chat API, how likely the efficient
  model is to solve the task. Below a probability of 0.6, the request goes to the capable model.

Each judge is called through a non-blocking Envoy HTTP callout. If a judge call fails, the request
goes to the capable model (`fail_open`). Agent Router then sends the request to the selected model
on your OpenAI-compatible endpoint, and the run records the real answers.

## Files

| File | Purpose |
| --- | --- |
| `gateway.yaml` | Gateway and EnvoyProxy (template). Adds static `typesafe` (`api.typesafe.ai:443`, TLS) and `inference` (your endpoint) clusters for the judges, mounts the `judge-keys` Secret at `/etc/switchyard/`, and writes a JSON access log line with the Switchyard decision for every request. |
| `switchyard-policy.yaml` | The Switchyard filter configuration (template): the `jev` and `classifier` judges and the two virtual models. |
| `routes.yaml` | Agent Router routes for the target models (template): one `AIServiceBackend` for your endpoint, with an API key `BackendSecurityPolicy` and, for https, a `BackendTLSPolicy`. |
| `cases.json` | Coding-agent prompts, each labelled with the tier a human would pick (`efficient` or `capable`). |
| `run.sh` | Renders the templates, sets up the cluster, sends every case to both virtual models, and writes `out/results.json`. |

## Requirements

- docker (with buildx), kind, kubectl, helm, curl, jq, and python3 (for the HTML report).
- Kubernetes with the ImageVolume feature, enabled by default since Kubernetes 1.35.
- An OpenAI-compatible endpoint that serves the two target models and the classifier model, and
  its API key, stored in a file outside the repo.
- A Jev API key from typesafe.ai, stored in a file outside the repo.

## Run

```sh
mkdir -p ~/.config/judge-routing
printf '%s' 'YOUR_INFERENCE_KEY' > ~/.config/judge-routing/inference.key
printf '%s' 'YOUR_JEV_KEY' > ~/.config/judge-routing/jev.key
chmod 600 ~/.config/judge-routing/*.key

INFERENCE_BASE_URL=https://llm.example.com/v1 \
INFERENCE_API_KEY_FILE=~/.config/judge-routing/inference.key \
JEV_API_KEY_FILE=~/.config/judge-routing/jev.key \
./run.sh
```

`run.sh` renders the templates into `out/manifests/`, creates the kind cluster
`switchyard-judges` (or reuses it), builds and loads the extension image, installs Envoy Gateway
and Agent Router, and creates the `judge-keys` and `inference-api-key` Secrets from the key files.
The keys never appear in the manifests, the command line, or the output. `./run.sh --render-only`
only renders the templates, which is handy to review them.

It then sends each prompt in `cases.json` to `coding-jev` and to `coding-classifier`, one request
at a time, with `max_tokens` 400. For each request it records the HTTP status, the end-to-end
latency and the answer, and reads the selected model, the reason and the judge's evidence from the
Envoy access log. The results go to `out/results.json`, and, when `report/build_report.py` exists,
an HTML report goes to `out/report.html`. git ignores `out/`.

Any OpenAI-compatible endpoint works, including a local GPU server: set `INFERENCE_BASE_URL` to its
base URL (for example `http://gpu-box.local:8000/v1`), and `EFFICIENT_MODEL`, `CAPABLE_MODEL` and
`CLASSIFIER_MODEL` to the model names it serves. A plain `http` URL is called without TLS.

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `INFERENCE_BASE_URL` | (required) | OpenAI-compatible base URL that serves the target models and the classifier, for example `https://llm.example.com/v1`. The port defaults to 443 for https and 80 for http; the path is the API prefix (`/chat/completions` is appended). |
| `INFERENCE_API_KEY_FILE` | (required) | File holding the API key of `INFERENCE_BASE_URL`, sent as `Authorization: Bearer`. |
| `JEV_API_KEY_FILE` | (required) | File holding the Jev API key. |
| `EFFICIENT_MODEL` | `gpt-6.1-terra` | Efficient target model. |
| `CAPABLE_MODEL` | `claude-opus-5-5` | Capable target model. |
| `CLASSIFIER_MODEL` | `claude-haiku-5-5` | Model of the LLM classifier judge. |
| `ANSWER_MAX_TOKENS` | `1200` | `max_tokens` of each demo request. |
| `REASONING_EFFORT` | `low` | `reasoning_effort` of each demo request; set it empty to omit the field. Reasoning models can spend a small budget entirely on hidden reasoning and return no answer text. |
| `CLUSTER` | `switchyard-judges` | kind cluster name. |
| `NODE_IMAGE`, `EG_VERSION`, `AR_VERSION`, `AR_REF` | as in `../agent-router/kind.sh` | Versions of kind, Envoy Gateway and Agent Router. |
| `KEEP_CLUSTER` | `true` | Set to `false` to delete the cluster when done. |
| `PORT` | `18081` | Local port forwarded to the Gateway. |

## Reading the results

Each case in `out/results.json` has the `expected_tier` from `cases.json` and, under `runs`, one
entry per judge with the selected model, the reason, the judge's evidence, the status, the
end-to-end latency (judge plus target model) and the answer. Agreement between the selected tier
and the label shows how well each judge matches a human label for these prompts; `run.sh` prints
it for both.

The 0.15 Jev cutoff was picked on a small probe, where Jev scored trivial edits between 0.03 and
0.11 and multi-service or concurrency work between 0.21 and 0.36. Calibrate the cutoff and the
classifier threshold on your own traffic before relying on them.
