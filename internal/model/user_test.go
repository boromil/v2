// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model // import "miniflux.app/v2/internal/model"

import "testing"

func TestPatchStripContentBeforeFirstHeadingUnchangedWhenNil(t *testing.T) {
	user := &User{StripContentBeforeFirstHeading: true}
	modificationRequest := UserModificationRequest{}

	modificationRequest.Patch(user)

	if !user.StripContentBeforeFirstHeading {
		t.Error(`expected StripContentBeforeFirstHeading to remain true when the modification request field is nil`)
	}
}

func TestPatchStripContentBeforeFirstHeadingTrue(t *testing.T) {
	user := &User{StripContentBeforeFirstHeading: false}
	value := true
	modificationRequest := UserModificationRequest{StripContentBeforeFirstHeading: &value}

	modificationRequest.Patch(user)

	if !user.StripContentBeforeFirstHeading {
		t.Error(`expected StripContentBeforeFirstHeading to be set to true`)
	}
}

func TestPatchStripContentBeforeFirstHeadingFalse(t *testing.T) {
	user := &User{StripContentBeforeFirstHeading: true}
	value := false
	modificationRequest := UserModificationRequest{StripContentBeforeFirstHeading: &value}

	modificationRequest.Patch(user)

	if user.StripContentBeforeFirstHeading {
		t.Error(`expected StripContentBeforeFirstHeading to be set to false`)
	}
}
