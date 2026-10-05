import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 构建产物直接输出到 Go 的内嵌目录，run.sh 构建后即随二进制分发
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../cmd/server/web',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
