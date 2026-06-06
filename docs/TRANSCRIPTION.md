# Local Transcription

SignalForge Hub can run a local CPU transcription worker as an optional Docker Compose profile. The API owns the queue and audio access; the worker only claims jobs through internal token-protected endpoints.

## Enable

Set a strong shared worker token in your deployment environment:

```sh
TRANSCRIPTION_WORKER_TOKEN=replace-with-a-long-random-value
TRANSCRIPTION_MODEL=base
TRANSCRIPTION_DEVICE=cpu
TRANSCRIPTION_COMPUTE_TYPE=int8
TRANSCRIPTION_MIN_DURATION_SECONDS=0.75
TRANSCRIPTION_CPU_THREADS=0
# Optional, but useful to avoid anonymous Hugging Face rate limits during model downloads.
HF_TOKEN=hf_your_token_here
```

Start the hub with the transcription profile:

```sh
docker compose --profile transcription up -d --build
```

For production/Plesk compose files, use the same profile:

```sh
docker compose --env-file .env -f docker-compose.plesk.yml --profile transcription up -d --build
```

## How It Works

- New calls are inserted into `call_transcripts` with `status='pending'`.
- Existing calls are backfilled into the queue in batches when the worker claims jobs.
- The worker claims one job, downloads that call audio, runs `faster-whisper`, then writes the transcript back.
- Call log search and CSV export include completed transcript text.

## Tuning

Start with CPU settings:

```sh
TRANSCRIPTION_MODEL=base
TRANSCRIPTION_DEVICE=cpu
TRANSCRIPTION_COMPUTE_TYPE=int8
```

Use `TRANSCRIPTION_MODEL=tiny` on low-power hosts or while draining a large queue. If quality is too weak, try `base` or `small` once the backlog is under control.

Very short scanner clips are expensive relative to their useful transcript quality. By default the API and worker mark clips shorter than `TRANSCRIPTION_MIN_DURATION_SECONDS=0.75` as skipped. The API applies this when calls arrive and at startup for pending backfill, so tiny clips do not sit in the visible queue. Set it to `0` to transcribe every clip, or raise it to `1.0`/`1.5` if the queue is growing faster than the CPU can drain it.

If the monitored system is English-only, set `TRANSCRIPTION_LANGUAGE=en` to skip automatic language detection. On CPU-only hosts, leave `TRANSCRIPTION_CPU_THREADS=0` for faster-whisper auto tuning, or set it to the number of CPU cores you want the worker to use.

Use the Talkgroups screen to choose which talkgroups are eligible for transcription. The `TX` flag is **opt-in**: only talkgroups with `TX` enabled are queued. With zero `TX` flags set, nothing is transcribed. Toggling `TX` off re-skips pending work for that talkgroup on save; API startup also applies the policy to any backlog.

If backlog still grows faster than the worker can process, add another worker, move to a larger CPU host, or switch to a hosted transcription provider later.

The model cache is stored in the `transcriber_models` Docker volume so models are not downloaded on every restart.

## Local Smoke Test

For a fast local test, use the tiny model and keep the hub URLs local so magic links and browser API calls stay on localhost:

```sh
TRANSCRIPTION_WORKER_TOKEN=local-transcription-token \
TRANSCRIPTION_MODEL=tiny \
APP_BASE_URL=http://localhost:3000 \
HUB_PUBLIC_URL=http://localhost:3000 \
MAILJET_API_KEY= \
MAILJET_SECRET_KEY= \
MAIL_FROM_EMAIL= \
docker compose --profile transcription up -d --build
```

Confirm the worker is running and the API has the transcription token enabled:

```sh
docker compose ps transcriber
docker compose exec -T api sh -c 'test -n "$TRANSCRIPTION_WORKER_TOKEN" && echo transcription-enabled'
```

The first transcriber image build can look stalled on Apple Silicon while `pip` downloads large arm64 wheels such as `av`, `ctranslate2`, `onnxruntime`, and `numpy`. To see detailed progress, build the worker image directly once:

```sh
docker build --progress=plain -t p7-scanner-transcriber:local ./transcriber
```

After the dependency layer is cached, normal compose rebuilds should be quick.

## Queue Status

Transcription is optional. Installs without `TRANSCRIPTION_WORKER_TOKEN` configured hide transcript queue status in the call log, so operators who do not have enough CPU/RAM for local transcription are not shown a permanent backlog.

New calls are inserted into `call_transcripts` as `pending` immediately. When `TRANSCRIPTION_WORKER_TOKEN` is configured, the API also backfills missing `call_transcripts` rows for older calls on startup so the call log can show queued, running, done, or failed instead of a blank transcript state.

If a deployed instance shows some calls as queued and others with no transcript status after enabling transcription, redeploy or restart the updated API container once. To inspect the queue directly:

```sh
docker compose exec -T postgres psql -U p7scanner -d p7scanner -c "select status, count(*) from call_transcripts group by status order by status;"
```

## Portainer and Plesk

The Plesk and production compose files use the published transcriber image instead of building from local source. Leave the transcription profile off for small hosts.

To enable transcription in Portainer/Plesk, set a strong `TRANSCRIPTION_WORKER_TOKEN` and enable the `transcription` compose profile. In Portainer this is usually done by adding:

```env
COMPOSE_PROFILES=transcription
TRANSCRIPTION_WORKER_TOKEN=replace-with-a-long-random-value
TRANSCRIPTION_MODEL=tiny
TRANSCRIPTION_COMPUTE_TYPE=int8
TRANSCRIPTION_LANGUAGE=en
TRANSCRIPTION_MIN_DURATION_SECONDS=0.75
TRANSCRIPTION_CPU_THREADS=0
```

Use `TRANSCRIPTION_MODEL=tiny` for low-power testing. Keep transcription disabled on installs that cannot spare the CPU/RAM.
