// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// editedEntity is the entity an edit command edits.
type editedEntity struct {
	// spaceID is the entity's Space, or for a Space its own ID; it is zero for a Component.
	spaceID       goclientnew.UUID
	id            goclientnew.UUID
	slug          string
	backingUnitID *goclientnew.UUID
}

// entityEdit is what the edit command of one entity type needs of it
// (docs/design/entity-backing-units.md section 13).
type entityEdit struct {
	parent *cobra.Command
	// entity names the type in messages, as in "change workflow".
	entity string
	// orgLevel marks a type in no Space, which takes no --space.
	orgLevel bool
	// resolve finds the entity a command line names.
	resolve func(ref string) (*editedEntity, error)
	// getDocument reads the entity's document.
	getDocument func(e *editedEntity) (*goclientnew.EntityDocument, error)
	// updateDocument applies an edit of the document and displays the entity it wrote. It reports
	// whether the entity changed since the document was read.
	updateDocument func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (conflict bool, err error)
	// applyFromBackingUnit updates the entity from what its backing Unit holds that it has not taken:
	// the bulk patch from backing Units of that entity alone.
	applyFromBackingUnit func(e *editedEntity) error
}

// addEntityEditCommand adds `cub <entity> edit` to the entity type's command.
func addEntityEditCommand(edit entityEdit) {
	command := edit.parent.Name()
	cmd := &cobra.Command{
		Use:   "edit <" + command + ">",
		Short: fmt.Sprintf("Edit a %s in your system's editor", edit.entity),
		Long: getCommandHelp(fmt.Sprintf(`Open a %[1]s in the editor named by the EDITOR environment variable, or vi if it is not
set, as a document: the fields an update can set, with the entities the %[1]s refers to named
rather than identified by ID. When the editor exits, the %[1]s is updated with what changed:
fields left alone keep what the %[1]s holds, and fields removed are cleared. If nothing changed,
no update is made. If the %[1]s changed since it was read, the update is refused; edit it again.

A %[1]s with a backing Unit is edited through the Unit: the edit is saved as a Revision of the
Unit, the Unit's triggers run, and the %[1]s is then updated from the Unit, unless they report
validation errors. The edit is refused when the Unit has changes the %[1]s has not taken, unless
--force is given, which applies them along with the edit.

An edit that cannot be saved is left in a file, whose name is reported.`, edit.entity),
			fmt.Sprintf(`
  # Edit a %[2]s
  cub %[1]s edit my-%[1]s

  # Edit it with another editor
  EDITOR="code --wait" cub %[1]s edit my-%[1]s`, command, edit.entity)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEntityEdit(edit, args[0])
		},
	}
	if !edit.orgLevel {
		enableOptionalSpace(cmd)
	}
	addStandardDisplayFlags(cmd)
	cmd.Flags().BoolVar(&editForce, "force", false, fmt.Sprintf(
		"edit the %s's backing Unit even though it has changes the %s has not taken, and apply them with the edit",
		edit.entity, edit.entity))
	edit.parent.AddCommand(cmd)
}

// runEntityEdit edits the entity ref names: through its backing Unit if it has one, and otherwise as
// the document the server renders.
func runEntityEdit(edit entityEdit, ref string) error {
	entity, err := edit.resolve(ref)
	if err != nil {
		return err
	}
	if entity.backingUnitID != nil {
		return editThroughBackingUnit(edit.entity, edit.parent.Name(), entity.slug, *entity.backingUnitID, func() error {
			return edit.applyFromBackingUnit(entity)
		})
	}

	document, err := edit.getDocument(entity)
	if err != nil {
		return err
	}
	edited, path, err := editInEditor([]byte(document.Document), "*.yaml")
	if err != nil {
		return err
	}
	if bytes.Equal([]byte(document.Document), edited) {
		os.Remove(path)
		fmt.Println("No changes made")
		return nil
	}
	conflict, err := edit.updateDocument(entity, goclientnew.EntityDocumentEdit{
		Base: document.Document, Document: string(edited), Version: document.Version})
	if err != nil {
		if conflict {
			err = errors.Wrapf(err, "the %s changed since it was read; edit it again", edit.entity)
		}
		return keptEdit(path, err)
	}
	os.Remove(path)
	return nil
}

// readDocument is the document a request for one answered with.
func readDocument[R cubapi.APIResponse](res R, err error, document func(R) *goclientnew.EntityDocument) (*goclientnew.EntityDocument, error) {
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return document(res), nil
}

// documentUpdated interprets the answer to an update from an edit of a document, displaying the
// entity written, and reports whether the entity changed since the document was read.
func documentUpdated[E ModelConstraint, R cubapi.APIResponse](entityName string, res R, err error, written func(R) *E,
	name func(*E) (slug, id string), display func(*E)) (bool, error) {
	if cubapi.IsAPIError(err, res) {
		return err == nil && res.StatusCode() == http.StatusConflict, cubapi.InterpretErrorGeneric(err, res)
	}
	entity := written(res)
	slug, id := name(entity)
	displayUpdateResults(entity, entityName, slug, id, display)
	return false, nil
}

// appliedFromBackingUnit interprets the answer to the bulk patch from backing Units of one entity.
func appliedFromBackingUnit[T any, R cubapi.APIResponse](entityName, where string, res R, err error,
	responses func(R) (*[]T, *[]T), getError func(*T) *goclientnew.ResponseError, getName func(*T) string) error {
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	responses200, responses207 := responses(res)
	return displayBulkGenericCreateOrUpdateResults(responses200, responses207, res.StatusCode(), entityName, "update", where,
		getError, getName)
}

// editForce is --force on the edit commands: edit an entity's backing Unit even though it holds
// changes the entity has not taken, and apply them with the edit.
var editForce bool

// editInEditor opens content in the editor the EDITOR environment variable names, or vi, in a
// temporary file named by pattern, and returns what the file holds when the editor exits and the
// file's path. The caller removes the file, or keeps it to say where an edit that could not be
// saved is.
func editInEditor(content []byte, pattern string) ([]byte, string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, "", err
	}
	path := tmpFile.Name()
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(path)
		return nil, "", err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(path)
		return nil, "", err
	}

	editor := "vi"
	if os.Getenv("EDITOR") != "" {
		editor = os.Getenv("EDITOR")
	}
	vargs := strings.Split(editor, " ")
	vargs = append(vargs, path)
	c := exec.Command(vargs[0], vargs[1:]...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	_ = c.Run()

	edited, err := os.ReadFile(path)
	if err != nil {
		os.Remove(path)
		return nil, "", err
	}
	return edited, path, nil
}

// keptEdit reports an edit that could not be saved, and where the edited file was left so that
// the edit is not lost.
func keptEdit(path string, err error) error {
	return errors.Wrapf(err, "the edit was not saved; it is in %s", path)
}

// editThroughBackingUnit edits an entity that has a backing Unit by editing the Unit
// (docs/design/entity-backing-units.md section 13.1): the edit is saved as a Revision of the Unit,
// the Unit's Triggers run, and apply then updates the entity from the Unit. It refuses a Unit whose
// head the entity has not taken, whose changes apply would take along with the edit, unless
// --force.
func editThroughBackingUnit(entityName, command, entitySlug string, unitID goclientnew.UUID, apply func() error) error {
	unitDetails, err := resolveUnit(unitID.String(), "", "*")
	if err != nil {
		return err
	}
	unit := unitDetails.Unit
	unitName := unit.Slug
	if unitDetails.Space != nil {
		unitName = unitDetails.Space.Slug + "/" + unit.Slug
	}
	if unit.HeadRevisionNum != unit.LastReleasedRevisionNum && !editForce {
		return fmt.Errorf("the %s's backing Unit %s has changes the %s has not taken: its head is Revision %d, and the %s "+
			"holds Revision %d; apply them with `cub %s update --patch --from-backing-units`, or use --force to edit "+
			"the Unit anyway and apply them with the edit",
			entityName, unitName, entityName, unit.HeadRevisionNum, entityName, unit.LastReleasedRevisionNum, command)
	}

	current, err := fetchUnitData(unit.SpaceID, unit.UnitID)
	if err != nil {
		return err
	}
	edited, path, err := editInEditor([]byte(current), "*.yaml")
	if err != nil {
		return err
	}
	if bytes.Equal([]byte(current), edited) {
		os.Remove(path)
		fmt.Println("No changes made")
		return nil
	}
	params, err := unitDataParams(fmt.Sprintf("Edit of %s %s", entityName, entitySlug), changeSetIDForDataWrite(unit))
	if err != nil {
		return keptEdit(path, err)
	}
	if _, err := putUnitData(unit.SpaceID, unit.UnitID, string(edited), params); err != nil {
		return keptEdit(path, err)
	}
	// The Unit holds the edit now, so the file is no longer the only copy of it.
	os.Remove(path)

	// The Unit's Triggers have to run before the entity can take the edit: until they have, the
	// Unit is awaiting them, and the ValidationErrors they report gate the apply.
	written, err := resolveUnit(unit.UnitID.String(), "", "*")
	if err != nil {
		return err
	}
	if err := awaitTriggersRemoval(written.Unit); err != nil {
		return err
	}
	if err := apply(); err != nil {
		return errors.Wrapf(err, "the edit is saved in the backing Unit %s, but the %s did not take it; "+
			"fix it with `cub unit data-edit`, then apply it with `cub %s update --patch --from-backing-units`",
			unitName, entityName, command)
	}
	return nil
}
