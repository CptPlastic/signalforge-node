import './style.css';

import { GetVersion, ListDevices, LoadConfig, OpenURL, SaveConfig, StartRecorder, StopRecorder } from '../wailsjs/go/main/App';
import type { main } from '../wailsjs/go/models';
import { EventsOn } from '../wailsjs/runtime/runtime';

type Field = keyof main.RecorderConfig;

const app = document.querySelector<HTMLDivElement>('#app')!;
const releasesApi = 'https://api.github.com/repos/CptPlastic/signalforge.org/releases/latest';
const downloadsUrl = 'https://signalforge.org/#recorder';

app.innerHTML = `
  <main class="shell">
    <header class="topbar">
      <div>
        <div class="eyebrow">SIGNALFORGE RECORDER</div>
        <h1>P7 RECORDER CONSOLE</h1>
      </div>
      <div class="status" id="status">IDLE</div>
    </header>

    <section class="update-banner" id="update-banner" hidden>
      <div>
        <div class="eyebrow">UPDATE AVAILABLE</div>
        <div id="update-copy">A newer recorder release is available.</div>
      </div>
      <button type="button" id="update-download">DOWNLOAD</button>
    </section>

    <section class="grid">
      <form class="panel form" id="config-form">
        <div class="panel-head">
          <span>// source</span>
          <button type="button" class="ghost" data-link="https://signalforge.org/#recorder">DOWNLOADS</button>
        </div>
        <label>Config Path<input data-field="configPath" autocomplete="off" /></label>
        <label>P7 Server URL<input data-field="baseUrl" autocomplete="off" /></label>
        <label>Source API Key<input data-field="sourceKey" autocomplete="off" /></label>

        <div class="split">
          <label>Audio Device<input data-field="device" autocomplete="off" placeholder="blank, index, or device name" /></label>
          <label>VOX Threshold<input data-field="threshold" type="number" min="1" /></label>
        </div>

        <div class="split three">
          <label>Silence MS<input data-field="silenceMs" type="number" min="1" /></label>
          <label>Min MS<input data-field="minDurationMs" type="number" min="1" /></label>
          <label>Max Sec<input data-field="maxDurationSec" type="number" min="1" /></label>
        </div>

        <div class="actions">
          <button type="button" id="save">SAVE CONFIG</button>
          <button type="button" id="devices">LIST DEVICES</button>
          <button type="button" id="start" class="primary">START</button>
          <button type="button" id="stop" class="danger">STOP</button>
        </div>

        <div class="panel-head sub">
          <span>// folder ingest</span>
        </div>
        <div class="split toggles">
          <label class="checkline"><input data-field="folderIngestEnabled" type="checkbox" />Enable Folder Mode</label>
          <label class="checkline"><input data-field="folderIngestReprocessProcessed" type="checkbox" />Replay Processed</label>
        </div>
        <label>Ingest Folder<input data-field="folderIngestDirectory" autocomplete="off" placeholder="folder to watch for wav/mp3" /></label>
        <label>Processed Folder<input data-field="folderIngestProcessedDirectory" autocomplete="off" placeholder="processed" /></label>
        <div class="split">
          <label>Poll MS<input data-field="folderIngestPollMs" type="number" min="100" /></label>
          <label>Stable MS<input data-field="folderIngestStableMs" type="number" min="100" /></label>
        </div>

        <div class="panel-head sub">
          <span>// canary heartbeat</span>
        </div>
        <div class="split toggles">
          <label class="checkline"><input data-field="canaryEnabled" type="checkbox" />Enable Canary</label>
        </div>
        <div class="split three">
          <label>Interval Sec<input data-field="canaryIntervalSec" type="number" min="1" /></label>
          <label>Talkgroup<input data-field="canaryTalkgroup" type="number" min="0" /></label>
          <label>Talkgroup Label<input data-field="canaryTalkgroupLabel" autocomplete="off" /></label>
        </div>

        <div class="panel-head sub">
          <span>// metadata</span>
        </div>
        <div class="split three">
          <label>System<input data-field="system" type="number" min="1" /></label>
          <label>Talkgroup<input data-field="talkgroup" type="number" min="0" /></label>
          <label>Frequency Hz<input data-field="frequency" type="number" min="0" /></label>
        </div>
        <div class="split">
          <label>System Label<input data-field="systemLabel" /></label>
          <label>Talkgroup Label<input data-field="talkgroupLabel" /></label>
        </div>
        <div class="split">
          <label>Group<input data-field="talkgroupGroup" /></label>
          <label>Tag<input data-field="talkgroupTag" /></label>
        </div>

      </form>

      <aside class="side">
        <section class="panel compact">
          <div class="panel-head"><span>// links</span></div>
          <button type="button" class="wide" data-link="https://p7scan.projectseven.us/">OPEN P7</button>
          <button type="button" class="wide" data-link="https://signalforge.org/#recorder">RECORDER DOWNLOADS</button>
          <button type="button" class="wide" data-link="https://github.com/CptPlastic/signalforge.org/issues/new">FEEDBACK</button>
          <button type="button" class="wide" data-link="https://github.com/sponsors/CptPlastic">DONATE</button>
        </section>

        <section class="panel compact grow">
          <div class="panel-head"><span>// devices</span></div>
          <pre class="device-list" id="device-list">No device scan yet.</pre>
        </section>
      </aside>
    </section>

    <section class="panel log-panel">
      <div class="panel-head"><span>// recorder log</span></div>
      <pre id="log"></pre>
    </section>
  </main>
`;

const statusEl = document.querySelector<HTMLDivElement>('#status')!;
const logEl = document.querySelector<HTMLPreElement>('#log')!;
const deviceListEl = document.querySelector<HTMLPreElement>('#device-list')!;
const updateBannerEl = document.querySelector<HTMLElement>('#update-banner')!;
const updateCopyEl = document.querySelector<HTMLDivElement>('#update-copy')!;
const numericFields = new Set<Field>([
  'timeoutSec', 'sampleRate', 'channels', 'blockMs', 'threshold', 'silenceMs',
  'minDurationMs', 'maxDurationSec', 'preRollMs', 'system', 'talkgroup', 'frequency',
  'folderIngestPollMs', 'folderIngestStableMs', 'canaryIntervalSec', 'canaryTalkgroup'
]);
const checkboxFields = new Set<Field>([
  'folderIngestEnabled', 'folderIngestReprocessProcessed', 'canaryEnabled'
]);

function appendLog(line: string) {
  logEl.textContent += `${line}\n`;
  logEl.scrollTop = logEl.scrollHeight;
}

function setStatus(value: string) {
  statusEl.textContent = value;
}

function versionParts(value: string) {
  const match = /(\d+)\.(\d+)\.(\d+)/.exec(value);
  if (!match) return null;
  return match.slice(1).map(Number);
}

function isNewerVersion(latest: string, current: string) {
  const latestParts = versionParts(latest);
  const currentParts = versionParts(current);
  if (!latestParts || !currentParts) return false;
  for (let index = 0; index < latestParts.length; index += 1) {
    if (latestParts[index] > currentParts[index]) return true;
    if (latestParts[index] < currentParts[index]) return false;
  }
  return false;
}

async function checkForUpdates() {
  try {
    const current = await GetVersion();
    if (!current || current === 'dev') {
      appendLog('UPDATE CHECK SKIPPED dev build');
      return;
    }
    const response = await fetch(releasesApi, { headers: { Accept: 'application/vnd.github+json' } });
    if (!response.ok) throw new Error(String(response.status));
    const release = await response.json() as { tag_name?: string; html_url?: string };
    const latest = release.tag_name || '';
    if (!isNewerVersion(latest, current)) {
      appendLog(`UPDATE CHECK OK current ${current}`);
      return;
    }
    updateCopyEl.textContent = `Installed ${current}. Latest ${latest}.`;
    updateBannerEl.hidden = false;
    appendLog(`UPDATE AVAILABLE ${latest}`);
  } catch (error) {
    appendLog(`UPDATE CHECK ERROR ${error}`);
  }
}

function inputs() {
  return Array.from(document.querySelectorAll<HTMLInputElement>('[data-field]'));
}

function readConfig(): main.RecorderConfig {
  const cfg: Record<string, string | number | boolean> = {};
  for (const input of inputs()) {
    const field = input.dataset.field as Field;
    if (checkboxFields.has(field)) {
      cfg[field] = input.checked;
    } else {
      cfg[field] = numericFields.has(field) ? Number(input.value || 0) : input.value;
    }
  }
  return cfg as unknown as main.RecorderConfig;
}

function writeConfig(cfg: main.RecorderConfig) {
  for (const input of inputs()) {
    const field = input.dataset.field as Field;
    const value = (cfg as unknown as Record<string, string | number | boolean>)[field];
    if (checkboxFields.has(field)) {
      input.checked = Boolean(value);
    } else {
      input.value = String(value ?? '');
    }
  }
}

async function saveConfig() {
  setStatus('SAVING');
  await SaveConfig(readConfig());
  setStatus('CONFIG SAVED');
}

async function listDevices() {
  setStatus('SCANNING');
  deviceListEl.textContent = 'Scanning devices...';
  try {
    const output = await ListDevices();
    deviceListEl.textContent = output || 'No devices returned.';
    setStatus('DEVICE SCAN OK');
  } catch (error) {
    deviceListEl.textContent = String(error);
    setStatus('DEVICE SCAN ERROR');
  }
}

async function startRecorder() {
  setStatus('STARTING');
  await StartRecorder(readConfig());
  setStatus('RUNNING');
}

async function stopRecorder() {
  await StopRecorder();
  setStatus('IDLE');
}

document.querySelector<HTMLButtonElement>('#save')!.addEventListener('click', () => saveConfig().catch((error) => appendLog(`ERROR ${error}`)));
document.querySelector<HTMLButtonElement>('#devices')!.addEventListener('click', () => listDevices().catch((error) => appendLog(`ERROR ${error}`)));
document.querySelector<HTMLButtonElement>('#start')!.addEventListener('click', () => startRecorder().catch((error) => { setStatus('START ERROR'); appendLog(`ERROR ${error}`); }));
document.querySelector<HTMLButtonElement>('#stop')!.addEventListener('click', () => stopRecorder().catch((error) => appendLog(`ERROR ${error}`)));

for (const button of Array.from(document.querySelectorAll<HTMLButtonElement>('[data-link]'))) {
  button.addEventListener('click', () => OpenURL(button.dataset.link || '').catch((error) => appendLog(`LINK ERROR ${error}`)));
}

document.querySelector<HTMLButtonElement>('#update-download')!.addEventListener('click', () => OpenURL(downloadsUrl).catch((error) => appendLog(`LINK ERROR ${error}`)));

EventsOn('recorder:log', (line: string) => appendLog(line));

LoadConfig('').then(writeConfig).catch((error) => appendLog(`LOAD ERROR ${error}`));
checkForUpdates();