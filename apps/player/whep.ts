const pause = (ms: number) => new Promise<void>(resolve => window.setTimeout(resolve, ms));

export function receive(video: HTMLVideoElement, endpoint: string, signal: (live: boolean) => void) {
  const lifetime = new AbortController();
  let retry = 500;

  async function run() {
    while (!lifetime.signal.aborted) {
      const attempt = new AbortController();
      const cancel = () => attempt.abort();
      lifetime.signal.addEventListener('abort', cancel, { once: true });
      const pc = new RTCPeerConnection();
      const stream = new MediaStream();
      let session: string | undefined;
      let lastFrame = performance.now();
      let frameId = 0;
      let watchdog = 0;
      let live = false;
      const timeout = window.setTimeout(cancel, 12000);
      const frame = () => {
        lastFrame = performance.now();
        if (!live) {
          live = true;
          window.clearTimeout(timeout);
          retry = 500;
          signal(true);
        }
        frameId = video.requestVideoFrameCallback(frame);
      };

      try {
        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.ontrack = ({ track }) => {
          stream.addTrack(track);
          track.addEventListener('ended', cancel, { once: true });
        };
        pc.onconnectionstatechange = () => {
          if (pc.connectionState === 'failed' || pc.connectionState === 'closed') cancel();
        };
        video.srcObject = stream;
        frameId = video.requestVideoFrameCallback(frame);
        await pc.setLocalDescription(await pc.createOffer());
        // Complete ICE gathering before POST so that no PATCH is required.
        const iceDeadline = performance.now() + 3000;
        while (pc.iceGatheringState !== 'complete' && performance.now() < iceDeadline && !attempt.signal.aborted) {
          await pause(50);
        }
        const response = await fetch(endpoint, {
          method: 'POST', headers: { 'Content-Type': 'application/sdp' },
          body: pc.localDescription!.sdp, signal: attempt.signal,
        });
        if (response.status !== 201) throw new Error('WHEP unavailable');
        const location = response.headers.get('Location');
        if (location) session = new URL(location, response.url).href;
        await pc.setRemoteDescription({ type: 'answer', sdp: await response.text() });
        void video.play().catch(() => {});
        watchdog = window.setInterval(() => {
          if (live && performance.now() - lastFrame > 5000 && !document.hidden) cancel();
        }, 1000);
        if (!attempt.signal.aborted) {
          await new Promise<void>(resolve => attempt.signal.addEventListener('abort', () => resolve(), { once: true }));
        }
      } catch {
        // An unavailable publisher or failed negotiation is retried below.
      } finally {
        window.clearTimeout(timeout);
        window.clearInterval(watchdog);
        video.cancelVideoFrameCallback(frameId);
        lifetime.signal.removeEventListener('abort', cancel);
        pc.onconnectionstatechange = null;
        pc.close();
        stream.getTracks().forEach(track => track.stop());
        video.srcObject = null;
        if (!lifetime.signal.aborted) signal(false);
        if (session) void fetch(session, { method: 'DELETE', keepalive: true }).catch(() => {});
      }
      if (!lifetime.signal.aborted) await pause(retry);
      retry = Math.min(retry * 2, 4000);
    }
  }

  void run();
  return () => lifetime.abort();
}
