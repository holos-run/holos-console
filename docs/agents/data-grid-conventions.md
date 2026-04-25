# Data Grid Conventions

This page is now a quick pointer. The source of record for ResourceGrid row
navigation, cell links, action propagation guards, URL state, and dense table
defaults is [Data Grid Architecture](data-grid-architecture.md).

The rule most often needed during implementation: set `Row.detailHref` on every
resource row that has a detail page. ResourceGrid then makes both the Resource
ID cell and the full row clickable. Any row action button must call
`e.stopPropagation()` before opening menus or dialogs.
