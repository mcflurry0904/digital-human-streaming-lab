// Node.js 22+ provides the WebSocket client.
const [url = 'ws://127.0.0.1:8080/api/audio/ws', count = '100'] = process.argv.slice(2);
const limit = Number(count);
if (!Number.isSafeInteger(limit) || limit < 2) throw new Error('Frame count must be an integer >= 2');

let session;
let previous;
let first;
let frames = 0;
let verified = false;
const socket = new WebSocket(url);
socket.binaryType = 'arraybuffer';
const timeout = setTimeout(() => fail(new Error('Timed out waiting for PCM')), limit * 40 + 15000);

function fail(error) {
  console.error(error.message);
  clearTimeout(timeout);
  process.exitCode = 1;
  socket.close();
}

socket.addEventListener('message', ({ data }) => {
  try {
    if (typeof data === 'string') {
      const event = JSON.parse(data);
      console.log(event);
      if (event.type === 'start') {
        if (event.format !== 's16le' || event.sampleRate !== 16000 || event.channels !== 1 || event.frameMs !== 20) {
          throw new Error('Unexpected audio format');
        }
        session = event.session;
        previous = undefined;
        frames = 0;
      }
      return;
    }
    if (data.byteLength !== 672) throw new Error('Expected 32-byte header and 640-byte PCM payload');
    const id = Buffer.from(data, 0, 16).toString('hex');
    const view = new DataView(data);
    const seq = view.getBigUint64(16, true);
    const timestamp = view.getBigUint64(24, true);
    if (id !== session || timestamp !== seq * 20n || (previous !== undefined && seq !== previous + 1n)) {
      throw new Error('Invalid session, sequence or timestamp');
    }
    const now = performance.now();
    if (!frames) first = now;
    previous = seq;
    if (++frames === limit) {
      const interval = (now - first) / (limit - 1);
      if (interval < 15 || interval > 30) throw new Error('Unexpected mean arrival interval: ' + interval);
      console.log('Verified', frames, 'PCM frames at', interval.toFixed(2), 'ms/frame');
      verified = true;
      clearTimeout(timeout);
      socket.close();
    }
  } catch (error) { fail(error); }
});
socket.addEventListener('error', event => fail(new Error(event.message || 'WebSocket connection failed')));
socket.addEventListener('close', () => {
  clearTimeout(timeout);
  if (!verified) process.exitCode = 1;
});
