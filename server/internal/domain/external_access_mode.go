package domain

type ExternalPrincipalAccessMode string

const (
	ExternalPrincipalAccessModeShared         ExternalPrincipalAccessMode = "external_shared"
	ExternalPrincipalAccessModeSharedReadOnly ExternalPrincipalAccessMode = "external_shared_readonly"
)

func IsValidExternalPrincipalAccessMode(mode ExternalPrincipalAccessMode) bool {
	return mode == ExternalPrincipalAccessModeShared || mode == ExternalPrincipalAccessModeSharedReadOnly
}
