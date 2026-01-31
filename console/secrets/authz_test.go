package secrets

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/holos-run/holos-console/console/rbac"
)

// defaultGM returns the default GroupMapping for tests.
func defaultGM() *rbac.GroupMapping {
	return rbac.NewGroupMapping(nil, nil, nil)
}

func TestCheckReadAccessSharing(t *testing.T) {
	gm := defaultGM()

	t.Run("user email grant allows read", func(t *testing.T) {
		err := CheckReadAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "viewer"}, nil)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("group grant allows read", func(t *testing.T) {
		err := CheckReadAccessSharing(gm, "bob@example.com", []string{"dev-team"},
			nil, map[string]string{"dev-team": "viewer"})
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("denies read without grants", func(t *testing.T) {
		err := CheckReadAccessSharing(gm, "carol@example.com", []string{"unknown"},
			nil, nil)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})
}

func TestCheckWriteAccessSharing(t *testing.T) {
	gm := defaultGM()

	t.Run("group grant allows write", func(t *testing.T) {
		err := CheckWriteAccessSharing(gm, "bob@example.com", []string{"writers"},
			nil, map[string]string{"writers": "editor"})
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("viewer grant denies write", func(t *testing.T) {
		err := CheckWriteAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "viewer"}, nil)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})
}

func TestCheckDeleteAccessSharing(t *testing.T) {
	gm := defaultGM()

	t.Run("owner email grant allows delete", func(t *testing.T) {
		err := CheckDeleteAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "owner"}, nil)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})
}

func TestCheckListAccessSharing(t *testing.T) {
	gm := defaultGM()

	t.Run("user email grant allows list", func(t *testing.T) {
		err := CheckListAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "viewer"}, nil)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})
}

func TestCheckAdminAccessSharing(t *testing.T) {
	gm := defaultGM()

	t.Run("owner email grant allows admin", func(t *testing.T) {
		err := CheckAdminAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "owner"}, nil)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("editor grant denies admin", func(t *testing.T) {
		err := CheckAdminAccessSharing(gm, "alice@example.com", nil,
			map[string]string{"alice@example.com": "editor"}, nil)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})
}

func TestPlatformRoleThroughWrappers(t *testing.T) {
	gm := defaultGM()

	t.Run("platform viewer grants read via wrapper", func(t *testing.T) {
		err := CheckReadAccessSharing(gm, "nobody@example.com", []string{"viewer"},
			nil, nil)
		if err != nil {
			t.Errorf("expected access granted via platform viewer role, got: %v", err)
		}
	})

	t.Run("platform editor grants write via wrapper", func(t *testing.T) {
		err := CheckWriteAccessSharing(gm, "nobody@example.com", []string{"editor"},
			nil, nil)
		if err != nil {
			t.Errorf("expected access granted via platform editor role, got: %v", err)
		}
	})

	t.Run("platform owner grants delete via wrapper", func(t *testing.T) {
		err := CheckDeleteAccessSharing(gm, "nobody@example.com", []string{"owner"},
			nil, nil)
		if err != nil {
			t.Errorf("expected access granted via platform owner role, got: %v", err)
		}
	})

	t.Run("platform viewer grants list via wrapper", func(t *testing.T) {
		err := CheckListAccessSharing(gm, "nobody@example.com", []string{"viewer"},
			nil, nil)
		if err != nil {
			t.Errorf("expected access granted via platform viewer role, got: %v", err)
		}
	})

	t.Run("platform owner grants admin via wrapper", func(t *testing.T) {
		err := CheckAdminAccessSharing(gm, "nobody@example.com", []string{"owner"},
			nil, nil)
		if err != nil {
			t.Errorf("expected access granted via platform owner role, got: %v", err)
		}
	})
}
