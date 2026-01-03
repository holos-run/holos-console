package rpc

import (
	"context"

	"connectrpc.com/connect"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

// VersionInfo holds version information to be returned by the service.
type VersionInfo struct {
	Version      string
	GitCommit    string
	GitTreeState string
	BuildDate    string
}

// VersionHandler implements the VersionService.
type VersionHandler struct {
	consolev1connect.UnimplementedVersionServiceHandler
	info VersionInfo
}

// NewVersionHandler creates a new VersionHandler with the provided version info.
func NewVersionHandler(info VersionInfo) *VersionHandler {
	return &VersionHandler{info: info}
}

// GetVersion returns the current server version.
func (h *VersionHandler) GetVersion(
	ctx context.Context,
	req *connect.Request[consolev1.GetVersionRequest],
) (*connect.Response[consolev1.GetVersionResponse], error) {
	resp := &consolev1.GetVersionResponse{
		Version:      h.info.Version,
		GitCommit:    h.info.GitCommit,
		GitTreeState: h.info.GitTreeState,
		BuildDate:    h.info.BuildDate,
	}
	return connect.NewResponse(resp), nil
}
