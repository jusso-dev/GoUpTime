import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/app/',
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8008',
      '/health': 'http://localhost:8008',
      '/s': 'http://localhost:8008',
    },
  },
  build: {
    outDir: '../internal/api/web/console/dist',
    emptyOutDir: true,
  },
});
