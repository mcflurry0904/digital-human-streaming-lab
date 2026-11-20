import { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Alert, Box, Button, Chip, Container, CssBaseline, Divider, LinearProgress, Paper,
  Stack, Switch, FormControlLabel, Tab, Tabs, Table, TableBody, TableCell, TableContainer,
  TableHead, TableRow, ThemeProvider, Typography, createTheme,
} from '@mui/material';

type Kind = 'audio' | 'video';
type Media = {
  id: string; name: string; size: number; duration: number; audioCodec: string;
  sampleRate?: number; channels?: number; videoCodec?: string; width?: number; height?: number;
};
type Status = {
  state: 'idle' | 'starting' | 'running' | 'stopped' | 'ended' | 'error';
  position: number; duration: number; clients: number; error?: string;
  mediaId?: string; loop: boolean;
};

const theme = createTheme({
  palette: {
    mode: 'dark',
    background: { default: '#10141c', paper: '#191f2b' },
    primary: { main: '#9bdcca' },
  },
  shape: { borderRadius: 12 },
  typography: {
    fontFamily: 'Inter, system-ui, sans-serif',
    button: { textTransform: 'none', fontWeight: 650 },
  },
});

const size = (bytes: number) => bytes >= 1048576 ? (bytes / 1048576).toFixed(1) + ' MiB' : Math.ceil(bytes / 1024) + ' KiB';
function duration(seconds: number) {
  const whole = Math.floor(seconds);
  return Math.floor(whole / 60) + ':' + String(whole % 60).padStart(2, '0');
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.error || 'Request failed (' + response.status + ')');
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}

function StreamConsole({ kind }: { kind: Kind }) {
  const root = '/api/' + kind;
  const [items, setItems] = useState<Media[]>([]);
  const [selected, setSelected] = useState('');
  const [status, setStatus] = useState<Status>();
  const [loop, setLoop] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [offline, setOffline] = useState('');
  const upload = useRef<HTMLInputElement>(null);
  const active = status?.state === 'running' || status?.state === 'starting';
  const current = items.find(item => item.id === selected);

  useEffect(() => {
    const lifetime = new AbortController();
    let timer = 0;
    async function refresh() {
      try {
        const state = await request<Status>(root + '/status', { signal: lifetime.signal });
        if (lifetime.signal.aborted) return;
        setStatus(state);
        setOffline('');
        const list = await request<Media[]>(root + '/media', { signal: lifetime.signal });
        if (lifetime.signal.aborted) return;
        setItems(list);
        setSelected(value => list.some(item => item.id === value) ? value :
          list.find(item => item.id === state.mediaId)?.id || list[0]?.id || '');
      } catch (problem) {
        if (lifetime.signal.aborted) return;
        setOffline(problem instanceof Error ? problem.message : 'Service unavailable');
      } finally {
        if (!lifetime.signal.aborted) timer = window.setTimeout(refresh, 1000);
      }
    }
    void refresh();
    return () => { lifetime.abort(); window.clearTimeout(timer); };
  }, [root]);

  async function perform(action: () => Promise<void>) {
    setBusy(true);
    setError('');
    try { await action(); }
    catch (problem) { setError(problem instanceof Error ? problem.message : 'Request failed'); }
    finally { setBusy(false); }
  }

  function control(action: 'start' | 'stop' | 'restart') {
    void perform(async () => {
      const state = await request<Status>(root + '/control/' + action, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mediaId: selected, loop }),
      });
      setStatus(state);
    });
  }

  return <Stack spacing={3}>
    {offline && <Alert severity="warning">{offline}</Alert>}
    {error && <Alert severity="error" onClose={() => setError('')}>{error}</Alert>}
    <Paper elevation={0} sx={{ p: { xs: 2, sm: 3 }, border: 1, borderColor: 'divider' }}>
      <Stack spacing={2.5}>
        <Stack direction="row" justifyContent="space-between" alignItems="center">
          <Typography component="h2" variant="h6">{kind === 'audio' ? 'Audio stream' : 'Mock video stream'}</Typography>
          <Chip label={offline ? 'Unavailable' : status?.state || 'Connecting'} color={offline ? 'warning' : active ? 'success' : status?.error ? 'error' : 'default'} />
        </Stack>
        {status?.error && <Alert severity="error">{status.error}</Alert>}
        <Box>
          <Stack direction="row" justifyContent="space-between" spacing={2} sx={{ mb: 1 }}>
            <Typography color="text.secondary">{items.find(item => item.id === status?.mediaId)?.name || 'No active source'}</Typography>
            <Typography sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
              {duration(status?.position || 0)} / {duration(status?.duration || 0)}
            </Typography>
          </Stack>
          <LinearProgress aria-label="Stream progress" variant="determinate" value={status?.duration ? 100 * status.position / status.duration : 0} />
        </Box>
        <Divider />
        <Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" spacing={1} alignItems={{ sm: 'center' }}>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Button variant="contained" disabled={busy || !!offline || !current || active} onClick={() => control('start')}>Start</Button>
            <Button variant="outlined" disabled={busy || !!offline || !active} onClick={() => control('stop')}>Stop</Button>
            <Button disabled={busy || !!offline || !current} onClick={() => control('restart')}>Restart</Button>
            <FormControlLabel control={<Switch checked={loop} onChange={event => setLoop(event.target.checked)} />} label="Loop" sx={{ ml: 0 }} />
          </Stack>
          {kind === 'audio' && <Typography color="text.secondary">{status?.clients ?? 0} subscribers</Typography>}
        </Stack>
      </Stack>
    </Paper>
    <Paper elevation={0} sx={{ border: 1, borderColor: 'divider' }}>
      <Stack sx={{ p: { xs: 2, sm: 3 } }} direction="row" justifyContent="space-between" alignItems="center">
        <Typography component="h2" variant="h6">{kind === 'audio' ? 'Audio files' : 'Video files'}</Typography>
        <Button variant="outlined" disabled={busy || !!offline} onClick={() => upload.current?.click()}>Upload {kind === 'audio' ? 'MP3 / WAV' : 'MP4'}</Button>
        <input ref={upload} type="file" hidden accept={kind === 'audio' ? '.mp3,.wav' : '.mp4'} onChange={event => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (file) void perform(async () => {
            const item = await request<Media>(root + '/media?name=' + encodeURIComponent(file.name), { method: 'POST', body: file });
            setItems(previous => [...previous.filter(existing => existing.id !== item.id), item]);
            setSelected(item.id);
          });
        }} />
      </Stack>
      <TableContainer>
        <Table size="small">
          <TableHead><TableRow><TableCell>Input</TableCell><TableCell>File</TableCell><TableCell>Media</TableCell><TableCell align="right">Actions</TableCell></TableRow></TableHead>
          <TableBody>
            {!items.length && <TableRow><TableCell colSpan={4}>No files uploaded</TableCell></TableRow>}
            {items.map(item => <TableRow key={item.id} selected={selected === item.id}>
              <TableCell><input type="radio" name={kind + '-input'} aria-label={'Select ' + item.name} checked={selected === item.id} onChange={() => setSelected(item.id)} /></TableCell>
              <TableCell sx={{ minWidth: 140, overflowWrap: 'anywhere' }}>
                {item.name}<Typography display="block" variant="caption" color="text.secondary">{size(item.size)} · {duration(item.duration)}</Typography>
              </TableCell>
              <TableCell sx={{ minWidth: 120 }}>
                {kind === 'audio' ? [item.sampleRate + ' Hz', item.channels + ' ch', item.audioCodec].join(' · ') :
                  [item.width + ' × ' + item.height, item.videoCodec, item.audioCodec].join(' · ')}
              </TableCell>
              <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                <Button href={root + '/media/' + item.id} size="small">Download</Button>
                <Button color="error" size="small" disabled={busy || !!offline || active && status?.mediaId === item.id} onClick={() => {
                  if (window.confirm('Delete ' + item.name + '?')) void perform(async () => {
                    await request(root + '/media/' + item.id, { method: 'DELETE' });
                    setItems(previous => previous.filter(existing => existing.id !== item.id));
                    setSelected(previous => previous === item.id ? '' : previous);
                  });
                }}>Delete</Button>
              </TableCell>
            </TableRow>)}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  </Stack>;
}

function App() {
  const [kind, setKind] = useState<Kind>('audio');
  return <ThemeProvider theme={theme}>
    <CssBaseline />
    <Container component="main" maxWidth="lg" sx={{ py: { xs: 3, sm: 6 } }}>
      <Typography component="h1" variant="h4" sx={{ fontWeight: 720, letterSpacing: '-0.035em' }}>Digital Human Streaming Lab</Typography>
      <Tabs value={kind} onChange={(_, value: Kind) => setKind(value)} aria-label="Stream controls" sx={{ my: 2.5 }}>
        <Tab value="audio" label="Audio control" /><Tab value="video" label="Mock video" />
      </Tabs>
      <StreamConsole key={kind} kind={kind} />
    </Container>
  </ThemeProvider>;
}

createRoot(document.getElementById('root')!).render(<App />);
