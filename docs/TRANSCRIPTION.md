# Local Transcription

SignalForge Hub can run a local CPU transcription worker as an optional Docker Compose profile. The API owns the queue and audio access; the worker only claims jobs through internal token-protected endpoints.

## Enable

Set a strong shared worker token in your deployment environment:

```sh
TRANSCRIPTION_WORKER_TOKEN=replace-with-a-long-random-value
TRANSCRIPTION_MODEL=base
TRANSCRIPTION_DEVICE=cpu
TRANSCRIPTION_COMPUTE_TYPE=int8
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

If quality is too weak, try `small`. If backlog grows faster than the worker can process, add another worker, move to a larger CPU host, or switch to a hosted transcription provider later.

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
