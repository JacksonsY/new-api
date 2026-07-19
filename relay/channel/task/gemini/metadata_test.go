package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Metadata may be written in the generateContent-style inlineData shape, but
// predictLongRunning only accepts bytesBase64Encoded/mimeType. These two tests
// pin the upstream wire format so the two shapes never get conflated again.
// They are split by input mode because Veo treats the modes as mutually
// exclusive — see TestApplyVeoMetadataToInstanceRejectsMixedInputModes.
func TestApplyVeoMetadataToInstanceSupportsFirstAndLastFrame(t *testing.T) {
	instance := VeoInstance{Prompt: "make a video"}
	features, err := ApplyVeoMetadataToInstance(map[string]any{
		"image": map[string]any{
			"inlineData": map[string]any{
				"mimeType": "image/png",
				"data":     "first-base64",
			},
		},
		"lastFrame": map[string]any{
			"inlineData": map[string]any{
				"mimeType": "image/png",
				"data":     "last-base64",
			},
		},
	}, &instance)

	require.NoError(t, err)
	require.True(t, features.HasImage)
	require.True(t, features.HasLastFrame)

	data, err := common.Marshal(VeoRequestPayload{Instances: []VeoInstance{instance}})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"instances": [{
			"prompt": "make a video",
			"image": {"bytesBase64Encoded": "first-base64", "mimeType": "image/png"},
			"lastFrame": {"bytesBase64Encoded": "last-base64", "mimeType": "image/png"}
		}]
	}`, string(data))
}

func TestApplyVeoMetadataToInstanceSupportsReferenceImages(t *testing.T) {
	instance := VeoInstance{Prompt: "make a video"}
	features, err := ApplyVeoMetadataToInstance(map[string]any{
		"referenceImages": []any{
			map[string]any{
				"image": map[string]any{
					"inlineData": map[string]any{
						"mimeType": "image/png",
						"data":     "asset-base64",
					},
				},
				"referenceType": "asset",
			},
		},
	}, &instance)

	require.NoError(t, err)
	require.True(t, features.HasReferenceImages)
	require.False(t, features.HasImage)

	data, err := common.Marshal(VeoRequestPayload{Instances: []VeoInstance{instance}})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"instances": [{
			"prompt": "make a video",
			"referenceImages": [{
				"image": {"bytesBase64Encoded": "asset-base64", "mimeType": "image/png"},
				"referenceType": "asset"
			}]
		}]
	}`, string(data))
}

// Veo rejects these combinations upstream: referenceImages cannot coexist with
// image/lastFrame, and lastFrame is only valid paired with image. Catch them
// locally instead of spending a round trip to learn the request was malformed.
func TestApplyVeoMetadataToInstanceRejectsMixedInputModes(t *testing.T) {
	image := map[string]any{
		"inlineData": map[string]any{"mimeType": "image/png", "data": "first-base64"},
	}
	referenceImages := []any{
		map[string]any{"image": image, "referenceType": "asset"},
	}

	tests := []struct {
		name        string
		metadata    map[string]any
		errContains string
	}{
		{
			name:        "reference images with image",
			metadata:    map[string]any{"image": image, "referenceImages": referenceImages},
			errContains: "referenceImages cannot be combined",
		},
		{
			name:        "reference images with last frame",
			metadata:    map[string]any{"lastFrame": image, "referenceImages": referenceImages},
			errContains: "referenceImages cannot be combined",
		},
		{
			name:        "last frame without image",
			metadata:    map[string]any{"lastFrame": image},
			errContains: "lastFrame requires image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := VeoInstance{Prompt: "make a video"}
			_, err := ApplyVeoMetadataToInstance(tt.metadata, &instance)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// The multipart upload path sets instance.Image before metadata is applied, so
// the exclusivity check must look at the instance, not just the metadata keys.
func TestApplyVeoMetadataToInstanceRejectsReferenceImagesOverMultipartImage(t *testing.T) {
	instance := VeoInstance{
		Prompt: "make a video",
		Image:  NewVeoImageInput("multipart-base64", "image/png"),
	}
	_, err := ApplyVeoMetadataToInstance(map[string]any{
		"referenceImages": []any{"data:image/png;base64,aVZCTw=="},
	}, &instance)

	require.Error(t, err)
	require.Contains(t, err.Error(), "referenceImages cannot be combined")
}

func TestApplyVeoMetadataToInstanceSupportsAliasesAndStringifiedJSON(t *testing.T) {
	instance := VeoInstance{Prompt: "make a video"}
	features, err := ApplyVeoMetadataToInstance(map[string]any{
		"first_frame": "data:image/png;base64,Zmlyc3Q=",
		"last_frame":  `{"inlineData":{"mimeType":"image/jpeg","data":"last-base64"}}`,
	}, &instance)

	require.NoError(t, err)
	require.True(t, features.HasImage)
	require.True(t, features.HasLastFrame)
	require.Equal(t, "image/png", instance.Image.MimeType)
	require.Equal(t, "Zmlyc3Q=", instance.Image.BytesBase64Encoded)
	require.Equal(t, "image/jpeg", instance.LastFrame.MimeType)
}

func TestApplyVeoMetadataToInstanceSupportsStringifiedReferenceImages(t *testing.T) {
	instance := VeoInstance{Prompt: "make a video"}
	features, err := ApplyVeoMetadataToInstance(map[string]any{
		"reference_images": `[{"image":{"inlineData":{"mimeType":"image/png","data":"asset-base64"}},"reference_type":"asset"}]`,
	}, &instance)

	require.NoError(t, err)
	require.True(t, features.HasReferenceImages)
	require.Len(t, instance.ReferenceImages, 1)
	require.Equal(t, "asset", instance.ReferenceImages[0].ReferenceType)
	require.Equal(t, "image/png", instance.ReferenceImages[0].Image.MimeType)
}

func TestApplyVeoMetadataToInstanceSupportsBareReferenceImageDataURI(t *testing.T) {
	instance := VeoInstance{Prompt: "make a video"}
	features, err := ApplyVeoMetadataToInstance(map[string]any{
		"referenceImages": []any{
			"data:image/png;base64,aVZCTw==",
			map[string]any{
				"inlineData": map[string]any{
					"mimeType": "image/jpeg",
					"data":     "bare-object-base64",
				},
			},
		},
	}, &instance)

	require.NoError(t, err)
	require.True(t, features.HasReferenceImages)
	require.Len(t, instance.ReferenceImages, 2)
	require.Equal(t, "asset", instance.ReferenceImages[0].ReferenceType)
	require.Equal(t, "image/png", instance.ReferenceImages[0].Image.MimeType)
	require.Equal(t, "aVZCTw==", instance.ReferenceImages[0].Image.BytesBase64Encoded)
	require.Equal(t, "asset", instance.ReferenceImages[1].ReferenceType)
	require.Equal(t, "image/jpeg", instance.ReferenceImages[1].Image.MimeType)
}

func TestApplyVeoMetadataToInstanceReturnsErrorsForInvalidInputs(t *testing.T) {
	tests := []struct {
		name        string
		metadata    map[string]any
		errContains string
	}{
		{
			name: "image missing inline data",
			metadata: map[string]any{
				"image": map[string]any{
					"inlineData": map[string]any{
						"mimeType": "image/png",
					},
				},
			},
			errContains: "inlineData.data is required",
		},
		{
			name: "reference images string is not array",
			metadata: map[string]any{
				"referenceImages": "data:image/png;base64,aVZCTw==",
			},
			errContains: "referenceImages string must contain a JSON array",
		},
		{
			name: "malformed json image string",
			metadata: map[string]any{
				"last_frame": `{"inlineData":{"mimeType":"image/png","data":"broken"`,
			},
			errContains: "invalid metadata lastFrame",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := VeoInstance{Prompt: "make a video"}
			features, err := ApplyVeoMetadataToInstance(tt.metadata, &instance)

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
			require.False(t, features.HasImage)
			require.False(t, features.HasLastFrame)
			require.False(t, features.HasReferenceImages)
			require.Nil(t, instance.Image)
			require.Nil(t, instance.LastFrame)
			require.Empty(t, instance.ReferenceImages)
			require.Equal(t, "make a video", instance.Prompt)
		})
	}
}
