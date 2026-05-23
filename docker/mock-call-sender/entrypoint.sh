#!/bin/sh
# mock-call-sender: simulates SDRTrunk sending calls to the scanner server.
#
# Each iteration generates a short sine-tone MP3 and POSTs it to
# POST /api/call-upload exactly as SDRTrunk (Rdio Scanner output plugin) would.
#
# Environment variables:
#   SCANNER_URL        Base URL of the hub API  (default: http://api:8080)
#   MOCK_INTERVAL_SEC  Seconds between calls           (default: 5)
#   MOCK_DURATION_SEC  Duration of each fake call      (default: 3)
#   MOCK_SYSTEM_ID     System ID                       (default: 1)
#   MOCK_SYSTEM_LABEL  System label                    (default: Mock P25 System)
#   MOCK_FREQUENCY     Base frequency in Hz            (default: 460000000)
#   MOCK_SOURCE_API_KEY Source API key for uploads     (preferred over the legacy source ID)

set -e

SCANNER_URL="${SCANNER_URL:-http://api:8080}"
INTERVAL="${MOCK_INTERVAL_SEC:-5}"
DURATION="${MOCK_DURATION_SEC:-3}"
SYSTEM_ID="${MOCK_SYSTEM_ID:-1}"
SYSTEM_LABEL="${MOCK_SYSTEM_LABEL:-Mock P25 System}"
BASE_FREQ="${MOCK_FREQUENCY:-460000000}"
SOURCE_KEY="${MOCK_SOURCE_API_KEY:-${MOCK_SOURCE_KEY:-mock-call-sender}}"

# Talkgroup definitions: id|label|group|tag
TALKGROUPS="
1001|DISPATCH ALPHA|FIRE|fire
1002|DISPATCH BRAVO|FIRE|fire
2001|PATROL NORTH|POLICE|law
2002|PATROL SOUTH|POLICE|law
3001|EMS UNIT 1|EMS|ems
"

pick_tg() {
    echo "$TALKGROUPS" | grep -v '^$' | awk -v seed="$RANDOM" 'BEGIN{srand(seed)} {lines[NR]=$0} END{print lines[int(rand()*NR)+1]}'
}

TMP_DIR="${TMPDIR:-/tmp}"

echo "[mock-call-sender] starting — targeting ${SCANNER_URL}"
echo "[mock-call-sender] interval=${INTERVAL}s  duration=${DURATION}s  system='${SYSTEM_LABEL}'"

# Wait for server to be ready before sending calls
until curl -sf "${SCANNER_URL}/api/v1/health" > /dev/null 2>&1; do
    echo "[mock-call-sender] waiting for server at ${SCANNER_URL} ..."
    sleep 2
done
echo "[mock-call-sender] server ready — sending calls"

SEQ=0
while true; do
    SEQ=$((SEQ + 1))

    TG_LINE="$(pick_tg)"
    TG_ID="$(echo "$TG_LINE"    | cut -d'|' -f1)"
    TG_LABEL="$(echo "$TG_LINE" | cut -d'|' -f2)"
    TG_GROUP="$(echo "$TG_LINE" | cut -d'|' -f3)"
    TG_TAG="$(echo "$TG_LINE"   | cut -d'|' -f4)"

    # Vary frequency slightly (25 kHz channel steps)
    FREQ_OFFSET=$(( (RANDOM % 40) * 25000 ))
    FREQ=$(( BASE_FREQ + FREQ_OFFSET ))

    # Vary tone per call so calls sound distinct (300-2000 Hz)
    TONE=$(( 300 + (RANDOM % 1700) ))

    TIMESTAMP="$(date +%s)"
    AUDIO_NAME="call_${TIMESTAMP}_tg${TG_ID}.mp3"
    AUDIO_FILE="${TMP_DIR}/${AUDIO_NAME}"

    # Generate a short MP3 using ffmpeg lavfi sine source
    ffmpeg -hide_banner -loglevel error \
        -f lavfi -i "sine=frequency=${TONE}:sample_rate=44100:duration=${DURATION}" \
        -c:a libmp3lame -b:a 64k -y \
        "${AUDIO_FILE}"

    # POST to the Rdio Scanner-compatible endpoint
    HTTP_STATUS="$(curl -s -o /dev/null -w "%{http_code}" \
        -F "system=${SYSTEM_ID}" \
        -F "systemLabel=${SYSTEM_LABEL}" \
        -F "talkgroup=${TG_ID}" \
        -F "talkgroupLabel=${TG_LABEL}" \
        -F "talkgroupGroup=${TG_GROUP}" \
        -F "talkgroupTag=${TG_TAG}" \
        -F "dateTime=${TIMESTAMP}" \
        -F "frequency=${FREQ}" \
        -F "duration=${DURATION}" \
        -F "audioName=${AUDIO_NAME}" \
        -F "audioType=audio/mpeg" \
        -F "key=${SOURCE_KEY}" \
        -F "audio=@${AUDIO_FILE};type=audio/mpeg" \
        "${SCANNER_URL}/api/call-upload" || echo "000")"

    rm -f "${AUDIO_FILE}"

    if [ "$HTTP_STATUS" = "200" ]; then
        echo "[mock-call-sender] #${SEQ} ok — tg=${TG_ID} (${TG_LABEL}) freq=${FREQ}Hz tone=${TONE}Hz"
    else
        echo "[mock-call-sender] #${SEQ} FAILED — tg=${TG_ID} http=${HTTP_STATUS}"
    fi

    sleep "${INTERVAL}"
done
