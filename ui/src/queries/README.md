# Queries

Use `@connectrpc/connect-query` hooks here to keep ConnectRPC calls and cache options
consistent across the UI.

Example:

```ts
import { create } from '@bufbuild/protobuf'
import { useQuery } from '@connectrpc/connect-query'
import { SomeService, SomeRequestSchema } from '../gen/holos/console/v1/some_pb.js'

export function useSomeQuery() {
  return useQuery(SomeService.method.someRpc, create(SomeRequestSchema))
}
```
