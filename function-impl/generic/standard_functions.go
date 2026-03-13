// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
)

func RegisterStandardFunctions(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	api.InitTypeSchemas()
	registerGetResources(fh, converter, resourceProvider)
	registerGetResourcesOfType(fh, converter, resourceProvider)
	registerGetReferencesOfType(fh, converter, resourceProvider)
	registerSetReferencesOfType(fh, converter, resourceProvider)
	registerGetPlaceholders(fh, converter, resourceProvider)
	registerGetPlaceholderMutations(fh, converter, resourceProvider)
	registerVetPlaceholders(fh, converter, resourceProvider)
	registerSearchReplace(fh, converter, resourceProvider)
	registerGetPath(fh, converter, resourceProvider)
	registerGetStringPath(fh, converter, resourceProvider)
	registerSetStringPath(fh, converter, resourceProvider)
	registerGetIntPath(fh, converter, resourceProvider)
	registerSetIntPath(fh, converter, resourceProvider)
	registerGetBoolPath(fh, converter, resourceProvider)
	registerSetBoolPath(fh, converter, resourceProvider)
	registerSetPathComment(fh, converter, resourceProvider)
	registerDeletePath(fh, converter, resourceProvider)
	registerSetDefaultNames(fh, converter, resourceProvider)
	registerGetAttribute(fh, converter, resourceProvider)
	registerSetAttributes(fh, converter, resourceProvider)
	registerGetNeeded(fh, converter, resourceProvider)
	registerGetProvided(fh, converter, resourceProvider)
	registerGetPaths(fh, converter, resourceProvider)
	registerVetCELExpr(fh, converter, resourceProvider)
	registerCELValidate(fh, converter, resourceProvider)
	registerWhereFilter(fh, converter, resourceProvider)
	registerSelectWhereResource(fh, converter, resourceProvider)
	registerYQ(fh, converter, resourceProvider)
	registerYQI(fh, converter, resourceProvider)
	registerIsApproved(fh, converter, resourceProvider)
	registerVetApprovedBy(fh, converter, resourceProvider)
	registerEnsureContext(fh, converter, resourceProvider)
	registerGetDetails(fh, converter, resourceProvider)
	registerUpsertResource(fh, converter, resourceProvider)
	registerDeleteResource(fh, converter, resourceProvider)
	registerPatchMutations(fh, converter, resourceProvider)
	registerReset(fh, converter, resourceProvider)
	registerReplicate(fh, converter, resourceProvider)
	registerVetJSONSchema(fh, converter, resourceProvider)
	registerNormalize(fh, converter, resourceProvider)
	registerSetStarlark(fh, converter, resourceProvider)
	registerVetStarlark(fh, converter, resourceProvider)
	registerGetStarlark(fh, converter, resourceProvider)
	registerVetCEL(fh, converter, resourceProvider)
	registerGetCEL(fh, converter, resourceProvider)
	registerSetCEL(fh, converter, resourceProvider)
	registerVetMergeKeys(fh, converter, resourceProvider)
	registerVetValues(fh, converter, resourceProvider)
}
