package auth

import "testing"

type metadataStorageStub struct {
	metadata map[string]any
}

func (s *metadataStorageStub) SaveTokenToFile(string) error { return nil }

func (s *metadataStorageStub) SetMetadata(metadata map[string]any) {
	s.metadata = make(map[string]any, len(metadata))
	for key, value := range metadata {
		s.metadata[key] = value
	}
}

func TestPrepareMetadataForPersistence_InjectsDisabledIntoStorageMetadata(t *testing.T) {
	storage := &metadataStorageStub{}
	auth := &Auth{
		Disabled: true,
		Storage:  storage,
		Metadata: map[string]any{"provider": "codex"},
	}

	PrepareMetadataForPersistence(auth)

	if auth.Metadata["disabled"] != true {
		t.Fatalf("expected auth metadata disabled=true, got %#v", auth.Metadata["disabled"])
	}
	if storage.metadata["disabled"] != true {
		t.Fatalf("expected storage metadata disabled=true, got %#v", storage.metadata["disabled"])
	}
}
