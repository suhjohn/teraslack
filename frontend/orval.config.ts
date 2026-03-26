import { defineConfig } from 'orval'

export default defineConfig({
  teraslack: {
    input: {
      target: '../server/api/openapi.yaml',
    },
    output: {
      mode: 'tags-split',
      target: 'src/lib/openapi/index.ts',
      schemas: 'src/lib/openapi/model',
      client: 'react-query',
      httpClient: 'fetch',
      override: {
        mutator: {
          path: './src/lib/orval-mutator.ts',
          name: 'orvalFetch',
        },
      },
    },
  },
})
