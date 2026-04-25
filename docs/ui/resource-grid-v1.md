# ResourceGrid v1

The ResourceGrid v1 design note has been consolidated into
[Data Grid Architecture](../agents/data-grid-architecture.md).

Use that architecture doc for the shell contract, column conventions, URL state,
toolbar/filter behavior, row-click rules, `extraColumns`, delete-confirm flow,
pagination and virtualization decision rules, and testing guidance.

Implementation lives in `frontend/src/components/resource-grid/`:

| File | Responsibility |
|---|---|
| `ResourceGrid.tsx` | Public shell and TanStack Table wiring |
| `Toolbar.tsx` | Search toolbar and create button/dropdown |
| `KindFilter.tsx` | Multi-kind checkbox filter |
| `useDeleteConfirm.ts` | Delete-confirm state and toast handling |
| `types.ts` | Public row/kind/search types plus extracted component props |
| `url-state.ts` | Search-param parsing and serialization |
