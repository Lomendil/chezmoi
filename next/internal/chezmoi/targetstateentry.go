package chezmoi

// FIXME I don't think we need to use lazyContents here, except the SHA256 stuff is useful

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"time"
)

// An EntryState represents the state of an entry.
type EntryState struct {
	// Type // FIXME
	Mode           os.FileMode `json:"mode" toml:"mode" yaml:"mode"`
	ContentsSHA256 hexBytes    `json:"contentsSHA256,omitempty" toml:"contentsSHA256" yaml:"contentsSHA256"`
}

// A TargetStateEntry represents the state of an entry in the target state.
type TargetStateEntry interface {
	Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error
	Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error)
	EntryState() (*EntryState, error)
	Evaluate() error
}

// A TargetStateAbsent represents the absence of an entry in the target state.
type TargetStateAbsent struct{}

// A TargetStateDir represents the state of a directory in the target state.
type TargetStateDir struct {
	perm os.FileMode
}

// A TargetStateFile represents the state of a file in the target state.
type TargetStateFile struct {
	*lazyContents
	perm os.FileMode
}

// A TargetStatePresent represents the presence of an entry in the target state.
type TargetStatePresent struct {
	*lazyContents
	perm os.FileMode
}

// A TargetStateRenameDir represents the renaming of a directory in the target
// state.
type TargetStateRenameDir struct {
	oldName string
	newName string
}

// A TargetStateScript represents the state of a script.
type TargetStateScript struct {
	*lazyContents
	name string
	once bool
}

// A TargetStateSymlink represents the state of a symlink in the target state.
type TargetStateSymlink struct {
	*lazyLinkname
}

// A scriptOnceState records the state of a script that should only be run once.
type scriptOnceState struct {
	Name  string    `json:"name" toml:"name" yaml:"name"`
	RunAt time.Time `json:"runAt" toml:"runAt" yaml:"runAt"`
}

// Equal returns true if es is equal to other.
func (es *EntryState) Equal(other *EntryState) bool {
	return es.Mode == other.Mode && bytes.Equal(es.ContentsSHA256, other.ContentsSHA256)
}

// Apply updates actualStateEntry to match t.
func (t *TargetStateAbsent) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	if _, ok := actualStateEntry.(*ActualStateAbsent); ok {
		return nil
	}
	return s.RemoveAll(actualStateEntry.Path())
}

// Equal returns true if actualStateEntry matches t.
func (t *TargetStateAbsent) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	_, ok := actualStateEntry.(*ActualStateAbsent)
	if !ok {
		return false, nil
	}
	return ok, nil
}

// EntryState returns t's entry state.
func (t *TargetStateAbsent) EntryState() (*EntryState, error) {
	return nil, nil // FIXME.
}

// Evaluate evaluates t.
func (t *TargetStateAbsent) Evaluate() error {
	return nil
}

// Apply updates actualStateEntry to match t. It does not recurse.
func (t *TargetStateDir) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	if actualStateDir, ok := actualStateEntry.(*ActualStateDir); ok {
		if umaskPermEqual(actualStateDir.perm, t.perm, umask) {
			return nil
		}
		return s.Chmod(actualStateDir.Path(), t.perm)
	}
	if err := actualStateEntry.Remove(s); err != nil {
		return err
	}
	return s.Mkdir(actualStateEntry.Path(), t.perm)
}

// Equal returns true if actualStateEntry matches t. It does not recurse.
func (t *TargetStateDir) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	actualStateDir, ok := actualStateEntry.(*ActualStateDir)
	if !ok {
		return false, nil
	}
	if !umaskPermEqual(actualStateDir.perm, t.perm, umask) {
		return false, nil
	}
	return true, nil
}

// EntryState returns t's entry state.
func (t *TargetStateDir) EntryState() (*EntryState, error) {
	return &EntryState{
		Mode: os.ModeDir | t.perm,
	}, nil
}

// Evaluate evaluates t.
func (t *TargetStateDir) Evaluate() error {
	return nil
}

// Apply updates actualStateEntry to match t.
func (t *TargetStateFile) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	if actualStateFile, ok := actualStateEntry.(*ActualStateFile); ok {
		// Compare file contents using only their SHA256 sums. This is so that
		// we can compare last-written states without storing the full contents
		// of each file written.
		actualContentsSHA256, err := actualStateFile.ContentsSHA256()
		if err != nil {
			return err
		}
		contentsSHA256, err := t.ContentsSHA256()
		if err != nil {
			return err
		}
		if bytes.Equal(actualContentsSHA256, contentsSHA256) {
			if umaskPermEqual(actualStateFile.perm, t.perm, umask) {
				return nil
			}
			return s.Chmod(actualStateFile.Path(), t.perm)
		}
	} else if err := actualStateEntry.Remove(s); err != nil {
		return err
	}
	contents, err := t.Contents()
	if err != nil {
		return err
	}
	return s.WriteFile(actualStateEntry.Path(), contents, t.perm)
}

// EntryState returns t's entry state.
func (t *TargetStateFile) EntryState() (*EntryState, error) {
	contentsSHA256, err := t.ContentsSHA256()
	if err != nil {
		return nil, err
	}
	return &EntryState{
		Mode:           t.perm,
		ContentsSHA256: hexBytes(contentsSHA256),
	}, nil
}

// Equal returns true if actualStateEntry matches t.
func (t *TargetStateFile) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	actualStateFile, ok := actualStateEntry.(*ActualStateFile)
	if !ok {
		return false, nil
	}
	if !umaskPermEqual(actualStateFile.perm, t.perm, umask) {
		return false, nil
	}
	actualContentsSHA256, err := actualStateFile.ContentsSHA256()
	if err != nil {
		return false, err
	}
	contentsSHA256, err := t.ContentsSHA256()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(actualContentsSHA256, contentsSHA256) {
		return false, nil
	}
	return true, nil
}

// Evaluate evaluates t.
func (t *TargetStateFile) Evaluate() error {
	_, err := t.ContentsSHA256()
	return err
}

// Apply updates actualStateEntry to match t.
func (t *TargetStatePresent) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	if actualStateFile, ok := actualStateEntry.(*ActualStateFile); ok {
		if umaskPermEqual(actualStateFile.perm, t.perm, umask) {
			return nil
		}
		return s.Chmod(actualStateFile.Path(), t.perm)
	} else if err := actualStateEntry.Remove(s); err != nil {
		return err
	}
	contents, err := t.Contents()
	if err != nil {
		return err
	}
	return s.WriteFile(actualStateEntry.Path(), contents, t.perm)
}

// EntryState returns t's entry state.
func (t *TargetStatePresent) EntryState() (*EntryState, error) {
	return nil, nil
}

// Equal returns true if actualStateEntry matches t.
func (t *TargetStatePresent) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	actualStateFile, ok := actualStateEntry.(*ActualStateFile)
	if !ok {
		return false, nil
	}
	if !umaskPermEqual(actualStateFile.perm, t.perm, umask) {
		return false, nil
	}
	return true, nil
}

// Evaluate evaluates t.
func (t *TargetStatePresent) Evaluate() error {
	_, err := t.ContentsSHA256()
	return err
}

// Apply renames actualStateEntry.
func (t *TargetStateRenameDir) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	dir := path.Dir(actualStateEntry.Path())
	return s.Rename(path.Join(dir, t.oldName), path.Join(dir, t.newName))
}

// Equal returns false because actualStateEntry has not been renamed.
func (t *TargetStateRenameDir) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	return false, nil
}

// EntryState returns t's entry state.
func (t *TargetStateRenameDir) EntryState() (*EntryState, error) {
	return nil, nil
}

// Evaluate does nothing.
func (t *TargetStateRenameDir) Evaluate() error {
	return nil
}

// Apply runs t.
func (t *TargetStateScript) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	var (
		key        []byte
		executedAt time.Time
	)
	if t.once {
		contentsSHA256, err := t.ContentsSHA256()
		if err != nil {
			return err
		}
		key = []byte(hex.EncodeToString(contentsSHA256))
		scriptOnceState, err := s.PersistentState().Get(ScriptOnceStateBucket, key)
		if err != nil {
			return err
		}
		if scriptOnceState != nil {
			return nil
		}
		executedAt = time.Now()
	}
	contents, err := t.Contents()
	if err != nil {
		return err
	}
	if isEmpty(contents) {
		return nil
	}
	if err := s.RunScript(t.name, path.Dir(actualStateEntry.Path()), contents); err != nil {
		return err
	}
	if t.once {
		value, err := json.Marshal(&scriptOnceState{
			Name:  t.name,
			RunAt: executedAt,
		})
		if err != nil {
			return err
		}
		if err := s.PersistentState().Set(ScriptOnceStateBucket, key, value); err != nil {
			return err
		}
	}
	return nil
}

// EntryState returns t's entry state.
func (t *TargetStateScript) EntryState() (*EntryState, error) {
	return nil, nil // FIXME
}

// Equal returns true if actualStateEntry matches t.
func (t *TargetStateScript) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	// Scripts are independent of the actual state.
	// FIXME maybe the destination state should store the sha256 sums of executed scripts
	return true, nil
}

// Evaluate evaluates t.
func (t *TargetStateScript) Evaluate() error {
	_, err := t.ContentsSHA256()
	return err
}

// Apply updates actualStateEntry to match t.
func (t *TargetStateSymlink) Apply(s System, actualStateEntry ActualStateEntry, umask os.FileMode) error {
	if actualStateSymlink, ok := actualStateEntry.(*ActualStateSymlink); ok {
		actualLinkname, err := actualStateSymlink.Linkname()
		if err != nil {
			return err
		}
		linkname, err := t.Linkname()
		if err != nil {
			return err
		}
		if actualLinkname == linkname {
			return nil
		}
	}
	linkname, err := t.Linkname()
	if err != nil {
		return err
	}
	if err := actualStateEntry.Remove(s); err != nil {
		return err
	}
	return s.WriteSymlink(linkname, actualStateEntry.Path())
}

// EntryState returns t's entry state.
func (t *TargetStateSymlink) EntryState() (*EntryState, error) {
	linknameSHA256, err := t.LinknameSHA256()
	if err != nil {
		return nil, err
	}
	return &EntryState{
		Mode:           os.ModeSymlink,
		ContentsSHA256: linknameSHA256,
	}, nil
}

// Equal returns true if actualStateEntry matches t.
func (t *TargetStateSymlink) Equal(actualStateEntry ActualStateEntry, umask os.FileMode) (bool, error) {
	actualStateSymlink, ok := actualStateEntry.(*ActualStateSymlink)
	if !ok {
		return false, nil
	}
	actualLinkname, err := actualStateSymlink.Linkname()
	if err != nil {
		return false, err
	}
	linkname, err := t.Linkname()
	if err != nil {
		return false, nil
	}
	if actualLinkname != linkname {
		return false, nil
	}
	return true, nil
}

// Evaluate evaluates t.
func (t *TargetStateSymlink) Evaluate() error {
	_, err := t.Linkname()
	return err
}
