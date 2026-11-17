package contentscan

import (
	"fmt"

	"sparkdream/tools/fileprofile"
)

// ErrFileProfile is returned (wrapped) by FileProfile for a file outside the
// storage profile of its declared type.
var ErrFileProfile = fileprofile.ErrOutsideProfile

// FileProfile reports whether data is a file x/storage will keep a permanent
// copy of under contentType (docs/x-storage-spec.md, "The file profile"): a
// PNG or JPEG holding one still image, with no transparency, no metadata and
// no colour management. A worker signs no verdict for a storage subject
// whose file is outside the profile, and a storage operator declines it as a
// mismatch. The rule itself lives in tools/fileprofile, which the chain
// binary's CLI links too; its answers are pinned by the vectors there.
//
// A panic in the parser is returned as a profile error. The bytes come from
// any member who pays a request fee, and every scanner and operator worker
// reads them: a parser bug must refuse one file, not stop the workers.
func FileProfile(contentType string, data []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%w: the check failed: %v", ErrFileProfile, rec)
		}
	}()
	return fileprofile.Check(contentType, data)
}
