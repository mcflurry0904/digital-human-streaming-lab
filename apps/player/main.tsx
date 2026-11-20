import { createRoot } from 'react-dom/client';
import { useEffect, useRef, useState } from 'react';
import { receive } from './whep';
import { openAudio } from './audio';
import './player.css';

function Player() {
  const screen = useRef<HTMLElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const [live, setLive] = useState(false);
  const audio = useRef<ReturnType<typeof openAudio> | null>(null);
  const [audioStatus, setAudioStatus] = useState('Connecting audio…');
  const [soundEnabled, setSoundEnabled] = useState(false);
  const [volume, setVolume] = useState(1);
  const [error, setError] = useState('');

  useEffect(() => {
    const config = fetch('/runtime.json').then(response => response.ok ? response.json() : null).catch(() => null);
    let closed = false;
    let stop: (() => void) | undefined;
    void config.then(data => {
      if (closed) return;
      if (typeof RTCPeerConnection === 'undefined' || !video.current?.requestVideoFrameCallback) {
        setError('This browser cannot play the live stream');
        return;
      }
      stop = receive(video.current, data?.whep || import.meta.env.VITE_WHEP_URL || '/gateway/live/whep', setLive);
    });
    return () => { closed = true; stop?.(); };
  }, []);

  useEffect(() => {
    const subscription = openAudio(setAudioStatus);
    audio.current = subscription;
    return () => { subscription.close(); audio.current = null; };
  }, []);

  async function enableSound() {
    if (!audio.current) return;
    try {
      await audio.current.resume();
      setSoundEnabled(true);
      setError('');
    } catch {
      setError('Sound unavailable');
    }
  }

  return <main ref={screen} className="screen">
    <video ref={video} muted playsInline />
    {!live && <div className="no-signal" role="status">NO SIGNAL</div>}
    <div className="controls">
      <span role="status">{audioStatus}</span>
      <button onClick={enableSound}>{soundEnabled ? 'Resume sound' : 'Enable sound'}</button>
      <input aria-label="Volume" type="range" min="0" max="1" step="0.01" value={volume} onChange={event => {
        const next = Number(event.target.value);
        audio.current?.setVolume(next);
        setVolume(next);
      }} />
      <button onClick={async () => {
        try {
          if (document.fullscreenElement) await document.exitFullscreen();
          else await screen.current!.requestFullscreen();
          setError('');
        } catch {
          setError('Fullscreen unavailable');
        }
      }}>Fullscreen</button>
    </div>
    {error && <div className="error" role="status">{error}</div>}
  </main>;
}
createRoot(document.getElementById('root')!).render(<Player />);
