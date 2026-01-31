# Observability

## Structured Log Attributes

### `resource_type`

Every audit log event includes a `resource_type` attribute identifying the
category of resource involved in the operation. This attribute enables faceted
search and filtering in log aggregation systems like Datadog.

| Value    | Package            | Description                    |
|----------|--------------------|--------------------------------|
| `secret` | `console/secrets`  | Secret CRUD and sharing events |

### Audit Log Actions

All secret audit events include both `action` and `resource_type` attributes:

| Action                  | Level | Description                        |
|-------------------------|-------|------------------------------------|
| `secrets_list`          | Info  | Secrets listed                     |
| `secret_access`         | Info  | Secret read access granted         |
| `secret_access_denied`  | Warn  | Secret read access denied          |
| `secret_create`         | Info  | Secret created                     |
| `secret_create_denied`  | Warn  | Secret creation denied             |
| `secret_update`         | Info  | Secret updated                     |
| `secret_update_denied`  | Warn  | Secret update denied               |
| `secret_delete`         | Info  | Secret deleted                     |
| `secret_delete_denied`  | Warn  | Secret deletion denied             |
| `sharing_update`        | Info  | Sharing grants updated             |
| `sharing_update_denied` | Warn  | Sharing grants update denied       |

## Datadog Queries

Filter all secret-related audit events:

```
@resource_type:secret
```

Filter denied operations only:

```
@resource_type:secret @level:warn
```

Filter by specific action:

```
@resource_type:secret @action:secret_access_denied
```

Filter by user email:

```
@resource_type:secret @email:alice\@example.com
```

## Datadog Facet Setup

1. Navigate to **Logs > Facets** in Datadog.
2. Click **Add Facet**.
3. Set the path to `@resource_type`.
4. Name the facet **Resource Type**.
5. Set the group to **Custom** (or a project-specific group).
6. Save.

Once created, `@resource_type` appears in the facet panel for filtering and
can be used in dashboards, monitors, and saved views.
