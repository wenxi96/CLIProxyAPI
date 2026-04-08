package auth

type metadataSetter interface {
	SetMetadata(map[string]any)
}

// PrepareMetadataForPersistence syncs runtime auth metadata into the persisted payload.
func PrepareMetadataForPersistence(auth *Auth) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		if auth.Storage == nil {
			return
		}
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["disabled"] = auth.Disabled
	if auth.Storage != nil {
		if setter, ok := auth.Storage.(metadataSetter); ok {
			setter.SetMetadata(auth.Metadata)
		}
	}
}
