package flow

import "errors"

// Conveniences the payload tests call; production code always passes client IDs.

// RPCUploadImage is the last captured ID of the image upload RPC (maseQ); the
// app discovers it at run time.
const RPCUploadImage = "maseQ"

// BuildImageBatchRequest constructs the verified batchexecute inner payload for RPC ogiZ0b (BatchGenerateImages)
// based on reverse-engineered JSPB AST from Google Flow web client. Every argument is
// required; nothing is replaced by a default.
func BuildImageBatchRequest(prompt string, projectID string, ratio string, modelID string, count int, recaptchaToken string, refMediaIDs ...[]string) ([]byte, error) {
	var refs []string
	if len(refMediaIDs) > 0 {
		refs = refMediaIDs[0]
	}
	return buildImageBatchRequest(prompt, projectID, ratio, modelID, count, recaptchaToken, refs, nil)
}

// ParseUploadedMediaID extracts the uploaded media ID from the maseQ payload. IDs the
// request itself carried (project ID, client UUIDs) are never mistaken for it.
func ParseUploadedMediaID(raw []byte, sent ...string) (string, error) {
	return parseUploadedMediaID(raw, RPCUploadImage, sent...)
}

// BuildVideoTextRequest builds the YhhmEf payload as captured by SolwStudio:
// [[item...], context, [batchID, 2]] where each item is
// [[null, null, [[[prompt]]]], variantKey, aspect, null, [null, null, null, null, clientID, clientID]].
// Flow's own client sends one item per requested output and, for any resolution
// other than 720p, adds field 8 = [resolution] (bundle builder u(); not in the capture).
func BuildVideoTextRequest(prompt, projectID, variantKey string, aspect, resolution, count int, recaptchaToken string) ([]byte, error) {
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, nil, recaptchaToken, nil)
}

// BuildVideoReferenceRequest builds the MZZa6b payload as captured by SolwStudio:
// each item is [[null, null, [[[prompt]]]], [[null, mediaID]...], variantKey, aspect, null, tailIDs];
// a resolution other than 720p goes in field 12 (bundle builder for t8a).
func BuildVideoReferenceRequest(prompt, projectID, variantKey string, aspect, resolution, count int, mediaIDs []string, recaptchaToken string) ([]byte, error) {
	if len(mediaIDs) == 0 {
		return nil, errors.New("thiếu ảnh tham chiếu")
	}
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, mediaIDs, recaptchaToken, nil)
}
