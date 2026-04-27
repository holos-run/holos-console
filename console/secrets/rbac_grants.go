package secrets

import (
	"strings"

	"github.com/holos-run/holos-console/console/oidc"
)

// UserIdentity maps a display email principal to the OIDC subject Kubernetes
// sees through ADR 036 impersonation.
type UserIdentity struct {
	Email   string
	Subject string
}

// RBACUserGrantsForSubjects converts email-shaped UI sharing grants into the
// subject-shaped principals used for Kubernetes RBAC when the subject is known.
// Unknown email principals are omitted because ADR 036 RBAC subjects must be
// OIDC sub values, not email addresses.
func RBACUserGrantsForSubjects(shareUsers []AnnotationGrant, identities ...UserIdentity) []AnnotationGrant {
	subjectsByEmail := make(map[string]string, len(identities)+4)
	for _, identity := range identities {
		addEmailSubject(subjectsByEmail, identity.Email, identity.Subject)
	}
	for _, user := range oidc.TestUsers {
		if subject, ok := oidc.TestUserSubjectForEmail(user.Email); ok {
			addEmailSubjectIfAbsent(subjectsByEmail, user.Email, subject)
		}
	}

	result := make([]AnnotationGrant, 0, len(shareUsers))
	for _, grant := range shareUsers {
		principal := strings.TrimSpace(grant.Principal)
		if principal == "" {
			continue
		}
		unprefixed := strings.TrimPrefix(principal, "oidc:")
		if strings.Contains(unprefixed, "@") {
			if subject := subjectsByEmail[strings.ToLower(unprefixed)]; subject != "" {
				grant.Principal = subject
			} else {
				continue
			}
		} else {
			grant.Principal = principal
		}
		result = append(result, grant)
	}
	return DeduplicateGrants(result)
}

func addEmailSubject(subjectsByEmail map[string]string, email, subject string) {
	email = strings.TrimSpace(email)
	subject = strings.TrimSpace(subject)
	if email == "" || subject == "" {
		return
	}
	subjectsByEmail[strings.ToLower(email)] = strings.TrimPrefix(subject, "oidc:")
}

func addEmailSubjectIfAbsent(subjectsByEmail map[string]string, email, subject string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	key := strings.ToLower(email)
	if subjectsByEmail[key] != "" {
		return
	}
	addEmailSubject(subjectsByEmail, email, subject)
}
