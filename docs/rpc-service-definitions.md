# RPC Service Definitions

This document describes how buf and ConnectRPC are used to generate Go structs and server bindings from protobuf definitions.

## Overview

The project uses:
- **Protobuf** for defining RPC services and messages
- **Buf** for linting, breaking change detection, and code generation
- **ConnectRPC** for the RPC framework (compatible with gRPC, gRPC-Web, and Connect protocols)

## Directory Structure

```
proto/                          # Protobuf source files
  holos/console/v1/
    version.proto               # Service and message definitions

gen/                            # Generated Go code (do not edit)
  holos/console/v1/
    version.pb.go               # Go structs for messages
    consolev1connect/
      version.connect.go        # ConnectRPC client and server bindings

console/rpc/                    # Hand-written RPC handlers
  version.go                    # VersionService implementation
```

## Configuration Files

### buf.yaml

Configures the buf module, linting rules, and breaking change detection:

```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

### buf.gen.yaml

Configures code generation plugins:

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - remote: buf.build/connectrpc/go
    out: gen
    opt: paths=source_relative
```

## Generating Code

Run buf to regenerate Go code after modifying proto files:

```bash
buf generate
```

This produces two files per proto file:
1. `*.pb.go` - Go structs for request/response messages
2. `*connect/*.connect.go` - ConnectRPC client and handler interfaces

## Adding a New RPC to an Existing Service

### Step 1: Define the RPC in the proto file

Edit the service definition in `proto/holos/console/v1/version.proto`:

```protobuf
service VersionService {
  // Existing RPC
  rpc GetVersion(GetVersionRequest) returns (GetVersionResponse);

  // New RPC
  rpc GetBuildInfo(GetBuildInfoRequest) returns (GetBuildInfoResponse);
}

// Add request message
message GetBuildInfoRequest {}

// Add response message
message GetBuildInfoResponse {
  string go_version = 1;
  string platform = 2;
}
```

### Step 2: Regenerate code

```bash
buf generate
```

### Step 3: Implement the handler

The generated `UnimplementedVersionServiceHandler` will have a new method. Implement it in your handler:

```go
// In console/rpc/version.go
func (h *VersionHandler) GetBuildInfo(
    ctx context.Context,
    req *connect.Request[consolev1.GetBuildInfoRequest],
) (*connect.Response[consolev1.GetBuildInfoResponse], error) {
    resp := &consolev1.GetBuildInfoResponse{
        GoVersion: runtime.Version(),
        Platform:  runtime.GOOS + "/" + runtime.GOARCH,
    }
    return connect.NewResponse(resp), nil
}
```

No changes needed to wire up the new RPC - it's automatically included when you registered the service handler.

## Adding a Field to an Existing Request or Response

### Step 1: Add the field to the proto message

Edit the message in `proto/holos/console/v1/version.proto`:

```protobuf
message GetVersionResponse {
  string version = 1;
  string git_commit = 2;
  string git_tree_state = 3;
  string build_date = 4;
  // New field - use the next available field number
  string go_version = 5;
}
```

**Important:** Never reuse or change existing field numbers. Always use the next sequential number.

### Step 2: Regenerate code

```bash
buf generate
```

### Step 3: Update the handler

Update your handler to populate the new field:

```go
func (h *VersionHandler) GetVersion(
    ctx context.Context,
    req *connect.Request[consolev1.GetVersionRequest],
) (*connect.Response[consolev1.GetVersionResponse], error) {
    resp := &consolev1.GetVersionResponse{
        Version:      h.info.Version,
        GitCommit:    h.info.GitCommit,
        GitTreeState: h.info.GitTreeState,
        BuildDate:    h.info.BuildDate,
        GoVersion:    runtime.Version(),  // New field
    }
    return connect.NewResponse(resp), nil
}
```

## Proto Best Practices

1. **Field numbers are forever** - Never change or reuse field numbers
2. **Use comments** - Document each field and RPC method
3. **Package naming** - Use `holos.console.v1` pattern for versioning
4. **go_package option** - Set to control the generated package name and import path

## Handler Pattern

Handlers embed the `Unimplemented*Handler` to satisfy the interface and provide forward compatibility:

```go
type VersionHandler struct {
    consolev1connect.UnimplementedVersionServiceHandler
    // ... handler dependencies
}
```

This allows new RPCs to be added to the proto without breaking existing handlers - unimplemented methods return `CodeUnimplemented`.
