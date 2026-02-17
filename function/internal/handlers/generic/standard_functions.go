// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
)

var (
	resourceListSchema         jsonschema.Schema
	resourceInfoListSchema     jsonschema.Schema
	attributeValueListSchema   jsonschema.Schema
	validationResultListSchema jsonschema.Schema
	yamlPayloadSchema          jsonschema.Schema
	resourceMutationListSchema jsonschema.Schema
)

func InitTypeSchemas() {
	var err error
	reflector := jsonschema.Reflector{}
	resourceListSchema, err = reflector.Reflect(api.ResourceList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceList")
	}
	resourceInfoListSchema, err = reflector.Reflect(api.ResourceInfoList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceInfoList")
	}
	attributeValueListSchema, err = reflector.Reflect(api.AttributeValueList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.AttributeValueList")
	}
	validationResultListSchema, err = reflector.Reflect(api.ValidationResultList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ValidationResultList")
	}
	yamlPayloadSchema, err = reflector.Reflect(api.YAMLPayload{})
	if err != nil {
		log.Errorf("couldn't get schema for api.YAMLPayload")
	}
	resourceMutationListSchema, err = reflector.Reflect(api.ResourceMutationList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceMutationList")
	}
}

func RegisterStandardFunctions(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	InitTypeSchemas()
	registerGetResources(fh, converter, resourceProvider)
	registerGetResourcesOfType(fh, converter, resourceProvider)
	registerSetReferencesOfType(fh, converter, resourceProvider)
	registerSetReferencesOfType(fh, converter, resourceProvider)
	registerGetPlaceholders(fh, converter, resourceProvider)
	registerGetPlaceholderMutations(fh, converter, resourceProvider)
	registerVetPlaceholders(fh, converter, resourceProvider)
	registerSearchReplace(fh, converter, resourceProvider)
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
	registerGetAttributes(fh, converter, resourceProvider)
	registerSetAttributes(fh, converter, resourceProvider)
	registerGetNeeded(fh, converter, resourceProvider)
	registerGetProvided(fh, converter, resourceProvider)
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
	RegisterComputeMutations(fh, converter, resourceProvider)
	registerPatchMutations(fh, converter, resourceProvider)
	registerReset(fh, converter, resourceProvider)
	registerReplicate(fh, converter, resourceProvider)
	registerVetJSONSchema(fh, converter, resourceProvider)
}
