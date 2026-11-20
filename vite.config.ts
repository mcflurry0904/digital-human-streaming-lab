import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const gateway = env.DEV_WHEP_TARGET || 'http://127.0.0.1:8889';
  return {
    envDir: process.cwd(),
    plugins: [react()],
    server: {
      port: mode === 'player' ? 5174 : 5173,
      proxy: {
        '/api/audio': {
          target: env.DEV_AUDIO_TARGET || 'http://127.0.0.1:8080',
          ws: true,
          rewrite: path => path.replace(/^\/api\/audio/, ''),
        },
        ...(env.DEV_VIDEO_TARGET ? { '/api/video': {
          target: env.DEV_VIDEO_TARGET,
          rewrite: (path: string) => path.replace(/^\/api\/video/, ''),
        } } : {}),
        '/gateway': {
          target: gateway,
          rewrite: path => path.replace(/^\/gateway/, ''),
          configure: proxy => proxy.on('proxyRes', response => {
            const location = response.headers.location;
            if (location) response.headers.location = '/gateway' + new URL(location, gateway).pathname;
          }),
        },
      },
    },
  };
});
