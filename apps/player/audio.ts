export function openAudio(onStatus: (status: string) => void) {
  let output: { context: AudioContext; gain: GainNode } | undefined;
  let volume = 1;

  const endpoint = new URL('/api/audio/ws', window.location.href);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  const pending = new Set<AudioBufferSourceNode>();
  let socket: WebSocket;
  let retry = 0;
  let handshake = 0;
  let closed = false;
  let session = '';
  let nextTime = 0;

  function clearQueue() {
    for (const source of pending) source.stop();
    pending.clear();
    nextTime = 0;
  }

  function connect() {
    socket = new WebSocket(endpoint);
    socket.binaryType = 'arraybuffer';
    handshake = window.setTimeout(() => socket.close(), 5000);
    socket.onmessage = ({ data }) => {
      if (closed) return;
      if (typeof data === 'string') {
        const event = JSON.parse(data);
        if (event.type === 'hello' && event.version === 1) {
          window.clearTimeout(handshake);
          onStatus('Audio connected');
        }
        if (event.type === 'start') {
          clearQueue();
          session = event.format === 's16le' && event.sampleRate === 16000 &&
            event.channels === 1 && event.frameBytes === 640 ? event.session : '';
        }
        if (event.type === 'end' && event.session === session) {
          session = '';
          if (event.reason !== 'ended') clearQueue();
        }
        return;
      }
      if (!session || !(data instanceof ArrayBuffer) || data.byteLength !== 672 ||
          output?.context.state !== 'running') return;
      const { context, gain } = output;
      const header = new Uint8Array(data, 0, 16);
      if (Array.from(header, byte => byte.toString(16).padStart(2, '0')).join('') !== session) return;

      const now = context.currentTime;
      if (nextTime > now + 0.25) clearQueue();
      if (nextTime < now + 0.005) nextTime = now + 0.06;
      const buffer = context.createBuffer(1, 320, 16000);
      const samples = buffer.getChannelData(0);
      const pcm = new DataView(data, 32);
      for (let index = 0; index < samples.length; index++) {
        samples[index] = pcm.getInt16(index * 2, true) / 32768;
      }
      const source = context.createBufferSource();
      source.buffer = buffer;
      source.connect(gain);
      source.onended = () => { pending.delete(source); source.disconnect(); };
      pending.add(source);
      source.start(nextTime);
      nextTime += buffer.duration;
    };
    socket.onclose = () => {
      session = '';
      clearQueue();
      window.clearTimeout(handshake);
      if (!closed) {
        onStatus('Audio disconnected · retrying');
        retry = window.setTimeout(connect, 1000);
      }
    };
  }

  connect();
  return {
    resume: () => {
      if (!output) {
        const context = new AudioContext();
        const gain = context.createGain();
        gain.gain.value = volume;
        gain.connect(context.destination);
        output = { context, gain };
      }
      return output.context.resume();
    },
    setVolume: (value: number) => {
      volume = value;
      if (output) output.gain.gain.value = value;
    },
    close: () => {
      closed = true;
      window.clearTimeout(retry);
      socket.close();
      clearQueue();
      window.clearTimeout(handshake);
      void output?.context.close();
    },
  };
}
