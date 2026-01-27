package secrets

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"
)

// CheckAccess verifies that the user has at least one group in common with the allowed groups.
// Returns nil if access is granted, or a PermissionDenied error otherwise.
func CheckAccess(userGroups, allowedGroups []string) error {
	for _, ug := range userGroups {
		for _, ag := range allowedGroups {
			if ug == ag {
				return nil
			}
		}
	}
	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied (not a member of: [%s])",
			strings.Join(allowedGroups, " ")),
	)
}
