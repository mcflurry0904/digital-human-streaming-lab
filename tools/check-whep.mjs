import assert from 'node:assert/strict';
import { setTimeout as sleep } from 'node:timers/promises';
import { receive } from '../apps/player/whep.ts';

const events = [];
const requests = [];
const peers = [];
let callback;
const video = {
  srcObject: null,
  muted: true,
  requestVideoFrameCallback(handler) { callback = handler; return 1; },
  cancelVideoFrameCallback() { callback = undefined; },
  async play() {},
};
globalThis.window = globalThis;
globalThis.document = { hidden: false };
globalThis.MediaStream = class {
  addTrack() {}
  getTracks() { return []; }
};
globalThis.RTCPeerConnection = class {
  iceGatheringState = 'complete';
  constructor() { peers.push(this); }
  addTransceiver() {}
  async createOffer() { return { sdp: 'offer' }; }
  async setLocalDescription(value) { this.localDescription = value; }
  async setRemoteDescription(value) {
    assert.equal(value.sdp, 'answer');
    setImmediate(() => callback?.());
  }
  close() { this.closed = true; }
};
let posts = 0;
globalThis.fetch = async (url, init) => {
  requests.push([String(url), init.method]);
  if (init.method === 'DELETE') return { status: 200 };
  assert.equal(init.body, 'offer');
  if (++posts === 1) return { status: 404 };
  return {
    status: 201, url: 'http://127.0.0.1/gateway/live/whep',
    headers: new Headers({ Location: '/gateway/live/whep/' + posts }),
    text: async () => 'answer',
  };
};

async function until(condition) {
  const deadline = performance.now() + 3500;
  while (!condition()) {
    if (performance.now() > deadline) throw Error('Timed out: ' + events);
    await sleep(20);
  }
}
const close = receive(video, '/gateway/live/whep', value => events.push(value));
try {
  await until(() => events.includes(true));
  assert.deepEqual(events, [false, true]);
  const peer = peers.at(-1);
  peer.connectionState = 'failed';
  peer.onconnectionstatechange();
  await until(() => events.length >= 4);
  assert.deepEqual(events, [false, true, false, true]);
  assert(requests.some(([url, method]) => url.endsWith('/whep/2') && method === 'DELETE'));
} finally { close(); }
await until(() => peers.every(peer => peer.closed));
assert.equal(video.srcObject, null);
assert(requests.some(([url, method]) => url.endsWith('/whep/3') && method === 'DELETE'));
console.log('Verified WHEP failure, retry, signal recovery and session cleanup with simulated WebRTC');
