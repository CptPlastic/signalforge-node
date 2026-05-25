# Local Transcription

SignalForge Hub can run a local CPU transcription worker as an optional Docker Compose profile. The API owns the queue and audio access; the worker only claims jobs through internal token-protected endpoints.

## Enable

Set a strong shared worker token in your deployment environment:

```sh
TRANSCRIPTION_WORKER_TOKEN=replace-with-a-long-random-value
TRANSCRIPTION_MODEL=base
TRANSCRIPTION_DEVICE=cpu
TRANSCRIPTION_COMPUTE_TYPE=int8
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
