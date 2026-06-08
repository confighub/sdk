// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT
import { confighubApi as api } from './confighubapi';

export const addTagTypes = [
  'Space',
  'Attribute',
  'BridgeWorker',
  'QueuedOperation',
  'ChangeSet',
  'Filter',
  'Function',
  'Meta',
  'Invocation',
  'Link',
  'UserInfo',
  'Organization',
  'OrganizationMember',
  'Revision',
  'BridgeWorkerStatus',
  'Tag',
  'Target',
  'Trigger',
  'Unit',
  'Mutation',
  'UnitAction',
  'UnitEvent',
  'View',
  'User',
] as const;
const injectedRtkApi = api
  .enhanceEndpoints({
    addTagTypes,
  })
  .injectEndpoints({
    endpoints: (build) => ({
      bulkDeleteSpaces: build.mutation<BulkDeleteSpacesApiResponse, BulkDeleteSpacesApiArg>({
        query: (queryArg) => ({
          url: `/_space`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            recursive: queryArg.recursive,
            recursive_force: queryArg.recursiveForce,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      bulkPatchSpaces: build.mutation<BulkPatchSpacesApiResponse, BulkPatchSpacesApiArg>({
        query: (queryArg) => ({
          url: `/_space`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      bulkCreateSpaces: build.mutation<BulkCreateSpacesApiResponse, BulkCreateSpacesApiArg>({
        query: (queryArg) => ({
          url: `/_space`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      bulkDeleteAttributes: build.mutation<
        BulkDeleteAttributesApiResponse,
        BulkDeleteAttributesApiArg
      >({
        query: (queryArg) => ({
          url: `/attribute`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Attribute'],
      }),
      listAllAttributes: build.query<ListAllAttributesApiResponse, ListAllAttributesApiArg>({
        query: (queryArg) => ({
          url: `/attribute`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Attribute'],
      }),
      bulkPatchAttributes: build.mutation<
        BulkPatchAttributesApiResponse,
        BulkPatchAttributesApiArg
      >({
        query: (queryArg) => ({
          url: `/attribute`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Attribute'],
      }),
      bulkCreateAttributes: build.mutation<
        BulkCreateAttributesApiResponse,
        BulkCreateAttributesApiArg
      >({
        query: (queryArg) => ({
          url: `/attribute`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Attribute'],
      }),
      bulkDeleteBridgeWorkers: build.mutation<
        BulkDeleteBridgeWorkersApiResponse,
        BulkDeleteBridgeWorkersApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      listAllBridgeWorkers: build.query<
        ListAllBridgeWorkersApiResponse,
        ListAllBridgeWorkersApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker`,
          params: {
            where: queryArg.where,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            summary: queryArg.summary,
          },
        }),
        providesTags: ['BridgeWorker'],
      }),
      bulkPatchBridgeWorkers: build.mutation<
        BulkPatchBridgeWorkersApiResponse,
        BulkPatchBridgeWorkersApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      createActionResult: build.mutation<
        CreateActionResultApiResponse,
        CreateActionResultApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker/${queryArg.bridgeWorkerId}/action_result`,
          method: 'POST',
          body: queryArg.actionResult,
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      getSelf: build.query<GetSelfApiResponse, GetSelfApiArg>({
        query: (queryArg) => ({ url: `/bridge_worker/${queryArg.bridgeWorkerId}/me` }),
        providesTags: ['BridgeWorker'],
      }),
      listQueuedOperations: build.query<
        ListQueuedOperationsApiResponse,
        ListQueuedOperationsApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker/${queryArg.bridgeWorkerId}/queued_operation`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['QueuedOperation'],
      }),
      getQueuedOperation: build.query<GetQueuedOperationApiResponse, GetQueuedOperationApiArg>(
        {
          query: (queryArg) => ({
            url: `/bridge_worker/${queryArg.bridgeWorkerId}/queued_operation/${queryArg.queuedOperationId}`,
          }),
          providesTags: ['QueuedOperation'],
        },
      ),
      streamBridgeWorker: build.mutation<
        StreamBridgeWorkerApiResponse,
        StreamBridgeWorkerApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker/${queryArg.bridgeWorkerId}/stream`,
          method: 'POST',
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      userCreateActionResult: build.mutation<
        UserCreateActionResultApiResponse,
        UserCreateActionResultApiArg
      >({
        query: (queryArg) => ({
          url: `/bridge_worker/${queryArg.bridgeWorkerId}/user_action_result`,
          method: 'POST',
          body: queryArg.actionResult,
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      bulkDeleteChangeSets: build.mutation<
        BulkDeleteChangeSetsApiResponse,
        BulkDeleteChangeSetsApiArg
      >({
        query: (queryArg) => ({
          url: `/change_set`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      listAllChangeSets: build.query<ListAllChangeSetsApiResponse, ListAllChangeSetsApiArg>({
        query: (queryArg) => ({
          url: `/change_set`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['ChangeSet'],
      }),
      bulkPatchChangeSets: build.mutation<
        BulkPatchChangeSetsApiResponse,
        BulkPatchChangeSetsApiArg
      >({
        query: (queryArg) => ({
          url: `/change_set`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      bulkCreateChangeSets: build.mutation<
        BulkCreateChangeSetsApiResponse,
        BulkCreateChangeSetsApiArg
      >({
        query: (queryArg) => ({
          url: `/change_set`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      bulkDeleteFilters: build.mutation<BulkDeleteFiltersApiResponse, BulkDeleteFiltersApiArg>(
        {
          query: (queryArg) => ({
            url: `/filter`,
            method: 'DELETE',
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
              include: queryArg.include,
            },
          }),
          invalidatesTags: ['Filter'],
        },
      ),
      listAllFilters: build.query<ListAllFiltersApiResponse, ListAllFiltersApiArg>({
        query: (queryArg) => ({
          url: `/filter`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            entity: queryArg.entity,
            id: queryArg.id,
          },
        }),
        providesTags: ['Filter'],
      }),
      bulkPatchFilters: build.mutation<BulkPatchFiltersApiResponse, BulkPatchFiltersApiArg>({
        query: (queryArg) => ({
          url: `/filter`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Filter'],
      }),
      bulkCreateFilters: build.mutation<BulkCreateFiltersApiResponse, BulkCreateFiltersApiArg>(
        {
          query: (queryArg) => ({
            url: `/filter`,
            method: 'POST',
            body: queryArg.body,
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
              include: queryArg.include,
              name_prefixes: queryArg.namePrefixes,
              variant_labels: queryArg.variantLabels,
              name_pattern: queryArg.namePattern,
              where_space: queryArg.whereSpace,
              filter_space: queryArg.filterSpace,
              allow_exists: queryArg.allowExists,
            },
          }),
          invalidatesTags: ['Filter'],
        },
      ),
      listOrgFunctions: build.query<ListOrgFunctionsApiResponse, ListOrgFunctionsApiArg>({
        query: (queryArg) => ({
          url: `/function`,
          params: {
            executor_space: queryArg.executorSpace,
            where: queryArg.where,
          },
        }),
        providesTags: ['Function'],
      }),
      invokeFunctionsOnOrg: build.mutation<
        InvokeFunctionsOnOrgApiResponse,
        InvokeFunctionsOnOrgApiArg
      >({
        query: (queryArg) => ({
          url: `/function/invoke`,
          method: 'POST',
          body: queryArg.functionInvocationsRequest,
          params: {
            executor_space: queryArg.executorSpace,
            dry_run: queryArg.dryRun,
            change_set_id: queryArg.changeSetId,
            subgroup: queryArg.subgroup,
            other_data_source: queryArg.otherDataSource,
            where: queryArg.where,
            filter: queryArg.filter,
            resource_type: queryArg.resourceType,
            where_data: queryArg.whereData,
            where_trigger: queryArg.whereTrigger,
            trigger_filter: queryArg.triggerFilter,
            triggers_passed: queryArg.triggersPassed,
            view: queryArg.view,
          },
        }),
        invalidatesTags: ['Function'],
      }),
      apiInfo: build.query<ApiInfoApiResponse, ApiInfoApiArg>({
        query: () => ({ url: `/info` }),
        providesTags: ['Meta'],
      }),
      bulkDeleteInvocations: build.mutation<
        BulkDeleteInvocationsApiResponse,
        BulkDeleteInvocationsApiArg
      >({
        query: (queryArg) => ({
          url: `/invocation`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Invocation'],
      }),
      listAllInvocations: build.query<ListAllInvocationsApiResponse, ListAllInvocationsApiArg>(
        {
          query: (queryArg) => ({
            url: `/invocation`,
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
              include: queryArg.include,
              select: queryArg.select,
            },
          }),
          providesTags: ['Invocation'],
        },
      ),
      bulkPatchInvocations: build.mutation<
        BulkPatchInvocationsApiResponse,
        BulkPatchInvocationsApiArg
      >({
        query: (queryArg) => ({
          url: `/invocation`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Invocation'],
      }),
      bulkCreateInvocations: build.mutation<
        BulkCreateInvocationsApiResponse,
        BulkCreateInvocationsApiArg
      >({
        query: (queryArg) => ({
          url: `/invocation`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Invocation'],
      }),
      bulkDeleteLinks: build.mutation<BulkDeleteLinksApiResponse, BulkDeleteLinksApiArg>({
        query: (queryArg) => ({
          url: `/link`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Link'],
      }),
      searchListLinks: build.query<SearchListLinksApiResponse, SearchListLinksApiArg>({
        query: (queryArg) => ({
          url: `/link`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Link'],
      }),
      bulkPatchLinks: build.mutation<BulkPatchLinksApiResponse, BulkPatchLinksApiArg>({
        query: (queryArg) => ({
          url: `/link`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            reverse: queryArg.reverse,
          },
        }),
        invalidatesTags: ['Link'],
      }),
      bulkCreateLinks: build.mutation<BulkCreateLinksApiResponse, BulkCreateLinksApiArg>({
        query: (queryArg) => ({
          url: `/link`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            reverse: queryArg.reverse,
            from_downstream_where: queryArg.fromDownstreamWhere,
            to_downstream_where: queryArg.toDownstreamWhere,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Link'],
      }),
      getMe: build.query<GetMeApiResponse, GetMeApiArg>({
        query: () => ({ url: `/me` }),
        providesTags: ['UserInfo'],
      }),
      listOrganizations: build.query<ListOrganizationsApiResponse, ListOrganizationsApiArg>({
        query: (queryArg) => ({
          url: `/organization`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Organization'],
      }),
      createOrganization: build.mutation<
        CreateOrganizationApiResponse,
        CreateOrganizationApiArg
      >({
        query: (queryArg) => ({
          url: `/organization`,
          method: 'POST',
          body: queryArg.organization,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Organization'],
      }),
      deleteOrganization: build.mutation<
        DeleteOrganizationApiResponse,
        DeleteOrganizationApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Organization'],
      }),
      getOrganization: build.query<GetOrganizationApiResponse, GetOrganizationApiArg>({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Organization'],
      }),
      updateOrganization: build.mutation<
        UpdateOrganizationApiResponse,
        UpdateOrganizationApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}`,
          method: 'PUT',
          body: queryArg.organization,
        }),
        invalidatesTags: ['Organization'],
      }),
      listOrganizationMembers: build.query<
        ListOrganizationMembersApiResponse,
        ListOrganizationMembersApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}/organization_member`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['OrganizationMember'],
      }),
      createOrganizationMember: build.mutation<
        CreateOrganizationMemberApiResponse,
        CreateOrganizationMemberApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}/organization_member`,
          method: 'POST',
          body: queryArg.organizationMember,
        }),
        invalidatesTags: ['OrganizationMember'],
      }),
      deleteOrganizationMember: build.mutation<
        DeleteOrganizationMemberApiResponse,
        DeleteOrganizationMemberApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}/organization_member/${queryArg.organizationMemberId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['OrganizationMember'],
      }),
      getOrganizationMember: build.query<
        GetOrganizationMemberApiResponse,
        GetOrganizationMemberApiArg
      >({
        query: (queryArg) => ({
          url: `/organization/${queryArg.organizationId}/organization_member/${queryArg.organizationMemberId}`,
        }),
        providesTags: ['OrganizationMember'],
      }),
      listAllRevisions: build.query<ListAllRevisionsApiResponse, ListAllRevisionsApiArg>({
        query: (queryArg) => ({
          url: `/revision`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Revision'],
      }),
      listSpaces: build.query<ListSpacesApiResponse, ListSpacesApiArg>({
        query: (queryArg) => ({
          url: `/space`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            summary: queryArg.summary,
          },
        }),
        providesTags: ['Space'],
      }),
      createSpace: build.mutation<CreateSpaceApiResponse, CreateSpaceApiArg>({
        query: (queryArg) => ({
          url: `/space`,
          method: 'POST',
          body: queryArg.space,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      deleteSpace: build.mutation<DeleteSpaceApiResponse, DeleteSpaceApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}`,
          method: 'DELETE',
          params: {
            recursive: queryArg.recursive,
            recursive_force: queryArg.recursiveForce,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      getSpace: build.query<GetSpaceApiResponse, GetSpaceApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
            summary: queryArg.summary,
          },
        }),
        providesTags: ['Space'],
      }),
      patchSpace: build.mutation<PatchSpaceApiResponse, PatchSpaceApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      updateSpace: build.mutation<UpdateSpaceApiResponse, UpdateSpaceApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}`,
          method: 'PUT',
          body: queryArg.space,
          params: {
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Space'],
      }),
      listAttributes: build.query<ListAttributesApiResponse, ListAttributesApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Attribute'],
      }),
      createAttribute: build.mutation<CreateAttributeApiResponse, CreateAttributeApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute`,
          method: 'POST',
          body: queryArg.attribute,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Attribute'],
      }),
      deleteAttribute: build.mutation<DeleteAttributeApiResponse, DeleteAttributeApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute/${queryArg.attributeId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Attribute'],
      }),
      getAttribute: build.query<GetAttributeApiResponse, GetAttributeApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute/${queryArg.attributeId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Attribute'],
      }),
      patchAttribute: build.mutation<PatchAttributeApiResponse, PatchAttributeApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute/${queryArg.attributeId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['Attribute'],
      }),
      updateAttribute: build.mutation<UpdateAttributeApiResponse, UpdateAttributeApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/attribute/${queryArg.attributeId}`,
          method: 'PUT',
          body: queryArg.attribute,
        }),
        invalidatesTags: ['Attribute'],
      }),
      listBridgeWorkers: build.query<ListBridgeWorkersApiResponse, ListBridgeWorkersApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['BridgeWorker'],
      }),
      createBridgeWorker: build.mutation<
        CreateBridgeWorkerApiResponse,
        CreateBridgeWorkerApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker`,
          method: 'POST',
          body: queryArg.bridgeWorker,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      deleteBridgeWorker: build.mutation<
        DeleteBridgeWorkerApiResponse,
        DeleteBridgeWorkerApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      getBridgeWorker: build.query<GetBridgeWorkerApiResponse, GetBridgeWorkerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['BridgeWorker'],
      }),
      patchBridgeWorker: build.mutation<PatchBridgeWorkerApiResponse, PatchBridgeWorkerApiArg>(
        {
          query: (queryArg) => ({
            url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}`,
            method: 'PATCH',
            body: queryArg.body,
          }),
          invalidatesTags: ['BridgeWorker'],
        },
      ),
      updateBridgeWorker: build.mutation<
        UpdateBridgeWorkerApiResponse,
        UpdateBridgeWorkerApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}`,
          method: 'PUT',
          body: queryArg.bridgeWorker,
        }),
        invalidatesTags: ['BridgeWorker'],
      }),
      listBridgeWorkerFunctions: build.query<
        ListBridgeWorkerFunctionsApiResponse,
        ListBridgeWorkerFunctionsApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}/function`,
        }),
        providesTags: ['BridgeWorker'],
      }),
      listBridgeWorkerStatuses: build.query<
        ListBridgeWorkerStatusesApiResponse,
        ListBridgeWorkerStatusesApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}/status`,
        }),
        providesTags: ['BridgeWorkerStatus'],
      }),
      getBridgeWorkerStatus: build.query<
        GetBridgeWorkerStatusApiResponse,
        GetBridgeWorkerStatusApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/bridge_worker/${queryArg.bridgeWorkerId}/status/${queryArg.statusId}`,
        }),
        providesTags: ['BridgeWorkerStatus'],
      }),
      listChangeSets: build.query<ListChangeSetsApiResponse, ListChangeSetsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['ChangeSet'],
      }),
      createChangeSet: build.mutation<CreateChangeSetApiResponse, CreateChangeSetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set`,
          method: 'POST',
          body: queryArg.changeSet,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      deleteChangeSet: build.mutation<DeleteChangeSetApiResponse, DeleteChangeSetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set/${queryArg.changeSetId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      getChangeSet: build.query<GetChangeSetApiResponse, GetChangeSetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set/${queryArg.changeSetId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['ChangeSet'],
      }),
      patchChangeSet: build.mutation<PatchChangeSetApiResponse, PatchChangeSetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set/${queryArg.changeSetId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      updateChangeSet: build.mutation<UpdateChangeSetApiResponse, UpdateChangeSetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/change_set/${queryArg.changeSetId}`,
          method: 'PUT',
          body: queryArg.changeSet,
        }),
        invalidatesTags: ['ChangeSet'],
      }),
      listFilters: build.query<ListFiltersApiResponse, ListFiltersApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            entity: queryArg.entity,
            id: queryArg.id,
          },
        }),
        providesTags: ['Filter'],
      }),
      createFilter: build.mutation<CreateFilterApiResponse, CreateFilterApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter`,
          method: 'POST',
          body: queryArg.filter,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Filter'],
      }),
      deleteFilter: build.mutation<DeleteFilterApiResponse, DeleteFilterApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter/${queryArg.filterId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Filter'],
      }),
      getFilter: build.query<GetFilterApiResponse, GetFilterApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter/${queryArg.filterId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Filter'],
      }),
      patchFilter: build.mutation<PatchFilterApiResponse, PatchFilterApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter/${queryArg.filterId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['Filter'],
      }),
      updateFilter: build.mutation<UpdateFilterApiResponse, UpdateFilterApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/filter/${queryArg.filterId}`,
          method: 'PUT',
          body: queryArg.filter,
        }),
        invalidatesTags: ['Filter'],
      }),
      listFunctions: build.query<ListFunctionsApiResponse, ListFunctionsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/function`,
          params: {
            entity: queryArg.entity,
            id: queryArg.id,
            where: queryArg.where,
          },
        }),
        providesTags: ['Function'],
      }),
      invokeFunctions: build.mutation<InvokeFunctionsApiResponse, InvokeFunctionsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/function/invoke`,
          method: 'POST',
          body: queryArg.functionInvocationsRequest,
          params: {
            unit_id: queryArg.unitId,
            revision_id: queryArg.revisionId,
            dry_run: queryArg.dryRun,
            change_set_id: queryArg.changeSetId,
            subgroup: queryArg.subgroup,
            other_data_source: queryArg.otherDataSource,
            where: queryArg.where,
            filter: queryArg.filter,
            resource_type: queryArg.resourceType,
            where_data: queryArg.whereData,
            where_trigger: queryArg.whereTrigger,
            trigger_filter: queryArg.triggerFilter,
            triggers_passed: queryArg.triggersPassed,
            view: queryArg.view,
          },
        }),
        invalidatesTags: ['Function'],
      }),
      listInvocations: build.query<ListInvocationsApiResponse, ListInvocationsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Invocation'],
      }),
      createInvocation: build.mutation<CreateInvocationApiResponse, CreateInvocationApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation`,
          method: 'POST',
          body: queryArg.invocation,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Invocation'],
      }),
      deleteInvocation: build.mutation<DeleteInvocationApiResponse, DeleteInvocationApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation/${queryArg.invocationId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Invocation'],
      }),
      getInvocation: build.query<GetInvocationApiResponse, GetInvocationApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation/${queryArg.invocationId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Invocation'],
      }),
      patchInvocation: build.mutation<PatchInvocationApiResponse, PatchInvocationApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation/${queryArg.invocationId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['Invocation'],
      }),
      updateInvocation: build.mutation<UpdateInvocationApiResponse, UpdateInvocationApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/invocation/${queryArg.invocationId}`,
          method: 'PUT',
          body: queryArg.invocation,
        }),
        invalidatesTags: ['Invocation'],
      }),
      listLinks: build.query<ListLinksApiResponse, ListLinksApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Link'],
      }),
      createLink: build.mutation<CreateLinkApiResponse, CreateLinkApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link`,
          method: 'POST',
          body: queryArg.link,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Link'],
      }),
      deleteLink: build.mutation<DeleteLinkApiResponse, DeleteLinkApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link/${queryArg.linkId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Link'],
      }),
      getLink: build.query<GetLinkApiResponse, GetLinkApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link/${queryArg.linkId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Link'],
      }),
      patchLink: build.mutation<PatchLinkApiResponse, PatchLinkApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link/${queryArg.linkId}`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            reverse: queryArg.reverse,
          },
        }),
        invalidatesTags: ['Link'],
      }),
      updateLink: build.mutation<UpdateLinkApiResponse, UpdateLinkApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/link/${queryArg.linkId}`,
          method: 'PUT',
          body: queryArg.link,
        }),
        invalidatesTags: ['Link'],
      }),
      listTags: build.query<ListTagsApiResponse, ListTagsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Tag'],
      }),
      createTag: build.mutation<CreateTagApiResponse, CreateTagApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag`,
          method: 'POST',
          body: queryArg.tag,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Tag'],
      }),
      deleteTag: build.mutation<DeleteTagApiResponse, DeleteTagApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag/${queryArg.tagId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Tag'],
      }),
      getTag: build.query<GetTagApiResponse, GetTagApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag/${queryArg.tagId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Tag'],
      }),
      patchTag: build.mutation<PatchTagApiResponse, PatchTagApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag/${queryArg.tagId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['Tag'],
      }),
      updateTag: build.mutation<UpdateTagApiResponse, UpdateTagApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/tag/${queryArg.tagId}`,
          method: 'PUT',
          body: queryArg.tag,
        }),
        invalidatesTags: ['Tag'],
      }),
      listTargets: build.query<ListTargetsApiResponse, ListTargetsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Target'],
      }),
      createTarget: build.mutation<CreateTargetApiResponse, CreateTargetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target`,
          method: 'POST',
          body: queryArg.target,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Target'],
      }),
      deleteTarget: build.mutation<DeleteTargetApiResponse, DeleteTargetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target/${queryArg.targetId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Target'],
      }),
      getTarget: build.query<GetTargetApiResponse, GetTargetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target/${queryArg.targetId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Target'],
      }),
      patchTarget: build.mutation<PatchTargetApiResponse, PatchTargetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target/${queryArg.targetId}`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Target'],
      }),
      updateTarget: build.mutation<UpdateTargetApiResponse, UpdateTargetApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/target/${queryArg.targetId}`,
          method: 'PUT',
          body: queryArg.target,
          params: {
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Target'],
      }),
      listTriggers: build.query<ListTriggersApiResponse, ListTriggersApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Trigger'],
      }),
      createTrigger: build.mutation<CreateTriggerApiResponse, CreateTriggerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger`,
          method: 'POST',
          body: queryArg.trigger,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Trigger'],
      }),
      deleteTrigger: build.mutation<DeleteTriggerApiResponse, DeleteTriggerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger/${queryArg.triggerId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Trigger'],
      }),
      getTrigger: build.query<GetTriggerApiResponse, GetTriggerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger/${queryArg.triggerId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Trigger'],
      }),
      patchTrigger: build.mutation<PatchTriggerApiResponse, PatchTriggerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger/${queryArg.triggerId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['Trigger'],
      }),
      updateTrigger: build.mutation<UpdateTriggerApiResponse, UpdateTriggerApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/trigger/${queryArg.triggerId}`,
          method: 'PUT',
          body: queryArg.trigger,
        }),
        invalidatesTags: ['Trigger'],
      }),
      listUnits: build.query<ListUnitsApiResponse, ListUnitsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            resource_type: queryArg.resourceType,
            where_data: queryArg.whereData,
            where_trigger: queryArg.whereTrigger,
            trigger_filter: queryArg.triggerFilter,
            triggers_passed: queryArg.triggersPassed,
            view: queryArg.view,
          },
        }),
        providesTags: ['Unit'],
      }),
      createUnit: build.mutation<CreateUnitApiResponse, CreateUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit`,
          method: 'POST',
          body: queryArg.unit,
          params: {
            upstream_space_id: queryArg.upstreamSpaceId,
            upstream_unit_id: queryArg.upstreamUnitId,
            merge_external_source: queryArg.mergeExternalSource,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      deleteUnit: build.mutation<DeleteUnitApiResponse, DeleteUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['Unit'],
      }),
      getUnit: build.query<GetUnitApiResponse, GetUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Unit'],
      }),
      patchUnit: build.mutation<PatchUnitApiResponse, PatchUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            revision_id: queryArg.revisionId,
            dry_run: queryArg.dryRun,
            upgrade: queryArg.upgrade,
            restore: queryArg.restore,
            resolve: queryArg.resolve,
            merge_source: queryArg.mergeSource,
            merge_base: queryArg.mergeBase,
            merge_end: queryArg.mergeEnd,
            merge_external_source: queryArg.mergeExternalSource,
            merge_disable_subtraction: queryArg.mergeDisableSubtraction,
            where_mutation: queryArg.whereMutation,
            filter_mutation: queryArg.filterMutation,
            tag: queryArg.tag,
            change_set_id: queryArg.changeSetId,
            subgroup: queryArg.subgroup,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      updateUnit: build.mutation<UpdateUnitApiResponse, UpdateUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}`,
          method: 'PUT',
          body: queryArg.unit,
          params: {
            revision_id: queryArg.revisionId,
            dry_run: queryArg.dryRun,
            upgrade: queryArg.upgrade,
            restore: queryArg.restore,
            resolve: queryArg.resolve,
            merge_source: queryArg.mergeSource,
            merge_base: queryArg.mergeBase,
            merge_end: queryArg.mergeEnd,
            merge_external_source: queryArg.mergeExternalSource,
            merge_disable_subtraction: queryArg.mergeDisableSubtraction,
            where_mutation: queryArg.whereMutation,
            filter_mutation: queryArg.filterMutation,
            tag: queryArg.tag,
            change_set_id: queryArg.changeSetId,
            subgroup: queryArg.subgroup,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      applyUnit: build.mutation<ApplyUnitApiResponse, ApplyUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/apply`,
          method: 'POST',
          params: {
            revision: queryArg.revision,
            dry_run: queryArg.dryRun,
            drift_mode: queryArg.driftMode,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      approveUnit: build.mutation<ApproveUnitApiResponse, ApproveUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/approve`,
          method: 'POST',
          params: {
            revision: queryArg.revision,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      downloadUnitData: build.query<DownloadUnitDataApiResponse, DownloadUnitDataApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/data`,
        }),
        providesTags: ['Unit'],
      }),
      destroyUnit: build.mutation<DestroyUnitApiResponse, DestroyUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/destroy`,
          method: 'POST',
          params: {
            dry_run: queryArg.dryRun,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      getUnitExtended: build.query<GetUnitExtendedApiResponse, GetUnitExtendedApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/extended`,
        }),
        providesTags: ['Unit'],
      }),
      importUnit: build.mutation<ImportUnitApiResponse, ImportUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/import`,
          method: 'POST',
          body: queryArg.importRequest,
          params: {
            dry_run: queryArg.dryRun,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      downloadUnitLiveData: build.query<
        DownloadUnitLiveDataApiResponse,
        DownloadUnitLiveDataApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/live_data`,
        }),
        providesTags: ['Unit'],
      }),
      downloadUnitLiveState: build.query<
        DownloadUnitLiveStateApiResponse,
        DownloadUnitLiveStateApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/live_state`,
        }),
        providesTags: ['Unit'],
      }),
      listExtendedMutations: build.query<
        ListExtendedMutationsApiResponse,
        ListExtendedMutationsApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/mutation`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Mutation'],
      }),
      getExtendedMutation: build.query<
        GetExtendedMutationApiResponse,
        GetExtendedMutationApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/mutation/${queryArg.mutationId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Mutation'],
      }),
      setUnitPredicates: build.mutation<SetUnitPredicatesApiResponse, SetUnitPredicatesApiArg>(
        {
          query: (queryArg) => ({
            url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/predicates`,
            method: 'POST',
            body: queryArg.unitPredicatesRequest,
          }),
          invalidatesTags: ['Unit'],
        },
      ),
      refreshUnit: build.mutation<RefreshUnitApiResponse, RefreshUnitApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/refresh`,
          method: 'POST',
          params: {
            dry_run: queryArg.dryRun,
            drift_mode: queryArg.driftMode,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      listExtendedRevisions: build.query<
        ListExtendedRevisionsApiResponse,
        ListExtendedRevisionsApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/revision`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Revision'],
      }),
      getExtendedRevision: build.query<
        GetExtendedRevisionApiResponse,
        GetExtendedRevisionApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/revision/${queryArg.revisionId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Revision'],
      }),
      downloadRevisionData: build.query<
        DownloadRevisionDataApiResponse,
        DownloadRevisionDataApiArg
      >({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/revision/${queryArg.revisionId}/data`,
        }),
        providesTags: ['Revision'],
      }),
      listUnitActions: build.query<ListUnitActionsApiResponse, ListUnitActionsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/unit_action`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['UnitAction'],
      }),
      getUnitAction: build.query<GetUnitActionApiResponse, GetUnitActionApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/unit_action/${queryArg.unitActionId}`,
        }),
        providesTags: ['UnitAction'],
      }),
      listUnitEvents: build.query<ListUnitEventsApiResponse, ListUnitEventsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/unit_event`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['UnitEvent'],
      }),
      getUnitEvent: build.query<GetUnitEventApiResponse, GetUnitEventApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/unit/${queryArg.unitId}/unit_event/${queryArg.unitEventId}`,
        }),
        providesTags: ['UnitEvent'],
      }),
      listViews: build.query<ListViewsApiResponse, ListViewsApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['View'],
      }),
      createView: build.mutation<CreateViewApiResponse, CreateViewApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view`,
          method: 'POST',
          body: queryArg.view,
          params: {
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['View'],
      }),
      deleteView: build.mutation<DeleteViewApiResponse, DeleteViewApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view/${queryArg.viewId}`,
          method: 'DELETE',
        }),
        invalidatesTags: ['View'],
      }),
      getView: build.query<GetViewApiResponse, GetViewApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view/${queryArg.viewId}`,
          params: {
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['View'],
      }),
      patchView: build.mutation<PatchViewApiResponse, PatchViewApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view/${queryArg.viewId}`,
          method: 'PATCH',
          body: queryArg.body,
        }),
        invalidatesTags: ['View'],
      }),
      updateView: build.mutation<UpdateViewApiResponse, UpdateViewApiArg>({
        query: (queryArg) => ({
          url: `/space/${queryArg.spaceId}/view/${queryArg.viewId}`,
          method: 'PUT',
          body: queryArg.view,
        }),
        invalidatesTags: ['View'],
      }),
      bulkDeleteTags: build.mutation<BulkDeleteTagsApiResponse, BulkDeleteTagsApiArg>({
        query: (queryArg) => ({
          url: `/tag`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Tag'],
      }),
      listAllTags: build.query<ListAllTagsApiResponse, ListAllTagsApiArg>({
        query: (queryArg) => ({
          url: `/tag`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Tag'],
      }),
      bulkPatchTags: build.mutation<BulkPatchTagsApiResponse, BulkPatchTagsApiArg>({
        query: (queryArg) => ({
          url: `/tag`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Tag'],
      }),
      bulkCreateTags: build.mutation<BulkCreateTagsApiResponse, BulkCreateTagsApiArg>({
        query: (queryArg) => ({
          url: `/tag`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Tag'],
      }),
      bulkDeleteTargets: build.mutation<BulkDeleteTargetsApiResponse, BulkDeleteTargetsApiArg>(
        {
          query: (queryArg) => ({
            url: `/target`,
            method: 'DELETE',
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
              include: queryArg.include,
            },
          }),
          invalidatesTags: ['Target'],
        },
      ),
      listAllTargets: build.query<ListAllTargetsApiResponse, ListAllTargetsApiArg>({
        query: (queryArg) => ({
          url: `/target`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Target'],
      }),
      bulkPatchTargets: build.mutation<BulkPatchTargetsApiResponse, BulkPatchTargetsApiArg>({
        query: (queryArg) => ({
          url: `/target`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            refresh_triggers: queryArg.refreshTriggers,
          },
        }),
        invalidatesTags: ['Target'],
      }),
      bulkDeleteTriggers: build.mutation<
        BulkDeleteTriggersApiResponse,
        BulkDeleteTriggersApiArg
      >({
        query: (queryArg) => ({
          url: `/trigger`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Trigger'],
      }),
      listAllTriggers: build.query<ListAllTriggersApiResponse, ListAllTriggersApiArg>({
        query: (queryArg) => ({
          url: `/trigger`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['Trigger'],
      }),
      bulkPatchTriggers: build.mutation<BulkPatchTriggersApiResponse, BulkPatchTriggersApiArg>(
        {
          query: (queryArg) => ({
            url: `/trigger`,
            method: 'PATCH',
            body: queryArg.body,
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
              include: queryArg.include,
            },
          }),
          invalidatesTags: ['Trigger'],
        },
      ),
      bulkCreateTriggers: build.mutation<
        BulkCreateTriggersApiResponse,
        BulkCreateTriggersApiArg
      >({
        query: (queryArg) => ({
          url: `/trigger`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['Trigger'],
      }),
      bulkDeleteUnits: build.mutation<BulkDeleteUnitsApiResponse, BulkDeleteUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      listAllUnits: build.query<ListAllUnitsApiResponse, ListAllUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
            resource_type: queryArg.resourceType,
            where_data: queryArg.whereData,
            where_trigger: queryArg.whereTrigger,
            trigger_filter: queryArg.triggerFilter,
            triggers_passed: queryArg.triggersPassed,
            view: queryArg.view,
          },
        }),
        providesTags: ['Unit'],
      }),
      bulkPatchUnits: build.mutation<BulkPatchUnitsApiResponse, BulkPatchUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            dry_run: queryArg.dryRun,
            upgrade: queryArg.upgrade,
            restore: queryArg.restore,
            resolve: queryArg.resolve,
            merge_source: queryArg.mergeSource,
            merge_base: queryArg.mergeBase,
            merge_end: queryArg.mergeEnd,
            merge_external_source: queryArg.mergeExternalSource,
            merge_disable_subtraction: queryArg.mergeDisableSubtraction,
            where_mutation: queryArg.whereMutation,
            filter_mutation: queryArg.filterMutation,
            tag: queryArg.tag,
            change_set_id: queryArg.changeSetId,
            subgroup: queryArg.subgroup,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkCreateUnits: build.mutation<BulkCreateUnitsApiResponse, BulkCreateUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
            include_outgoing_links_where: queryArg.includeOutgoingLinksWhere,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkApplyUnits: build.mutation<BulkApplyUnitsApiResponse, BulkApplyUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/apply`,
          method: 'POST',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            dry_run: queryArg.dryRun,
            revision: queryArg.revision,
            drift_mode: queryArg.driftMode,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkApproveUnits: build.mutation<BulkApproveUnitsApiResponse, BulkApproveUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/approve`,
          method: 'POST',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            revision: queryArg.revision,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkCancelUnits: build.mutation<BulkCancelUnitsApiResponse, BulkCancelUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/cancel`,
          method: 'POST',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkDestroyUnits: build.mutation<BulkDestroyUnitsApiResponse, BulkDestroyUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/destroy`,
          method: 'POST',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            dry_run: queryArg.dryRun,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkRefreshUnits: build.mutation<BulkRefreshUnitsApiResponse, BulkRefreshUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/refresh`,
          method: 'POST',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            dry_run: queryArg.dryRun,
            drift_mode: queryArg.driftMode,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      bulkTagUnits: build.mutation<BulkTagUnitsApiResponse, BulkTagUnitsApiArg>({
        query: (queryArg) => ({
          url: `/unit/tag`,
          method: 'POST',
          body: queryArg.unitTagRequest,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['Unit'],
      }),
      listAllUnitActions: build.query<ListAllUnitActionsApiResponse, ListAllUnitActionsApiArg>(
        {
          query: (queryArg) => ({
            url: `/unit_action`,
            params: {
              where: queryArg.where,
              filter: queryArg.filter,
              contains: queryArg.contains,
            },
          }),
          providesTags: ['QueuedOperation'],
        },
      ),
      listAllUnitEvents: build.query<ListAllUnitEventsApiResponse, ListAllUnitEventsApiArg>({
        query: (queryArg) => ({
          url: `/unit_event`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['UnitEvent'],
      }),
      listUsers: build.query<ListUsersApiResponse, ListUsersApiArg>({
        query: (queryArg) => ({
          url: `/user`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
          },
        }),
        providesTags: ['User'],
      }),
      getUser: build.query<GetUserApiResponse, GetUserApiArg>({
        query: (queryArg) => ({ url: `/user/${queryArg.userId}` }),
        providesTags: ['User'],
      }),
      bulkDeleteViews: build.mutation<BulkDeleteViewsApiResponse, BulkDeleteViewsApiArg>({
        query: (queryArg) => ({
          url: `/view`,
          method: 'DELETE',
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['View'],
      }),
      listAllViews: build.query<ListAllViewsApiResponse, ListAllViewsApiArg>({
        query: (queryArg) => ({
          url: `/view`,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            select: queryArg.select,
          },
        }),
        providesTags: ['View'],
      }),
      bulkPatchViews: build.mutation<BulkPatchViewsApiResponse, BulkPatchViewsApiArg>({
        query: (queryArg) => ({
          url: `/view`,
          method: 'PATCH',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
          },
        }),
        invalidatesTags: ['View'],
      }),
      bulkCreateViews: build.mutation<BulkCreateViewsApiResponse, BulkCreateViewsApiArg>({
        query: (queryArg) => ({
          url: `/view`,
          method: 'POST',
          body: queryArg.body,
          params: {
            where: queryArg.where,
            filter: queryArg.filter,
            contains: queryArg.contains,
            include: queryArg.include,
            name_prefixes: queryArg.namePrefixes,
            variant_labels: queryArg.variantLabels,
            name_pattern: queryArg.namePattern,
            where_space: queryArg.whereSpace,
            filter_space: queryArg.filterSpace,
            allow_exists: queryArg.allowExists,
          },
        }),
        invalidatesTags: ['View'],
      }),
    }),
    overrideExisting: false,
  });
export { injectedRtkApi as confighubApi };
export type BulkDeleteSpacesApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteSpacesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Space include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Space are AttributeFilterID, AttributeIDs, OrganizationID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Valid values are true and false. False is the default if unspecified. If true, recursively delete all entities within the deleted space(s) so long as none have delete gates. */
  recursive?: string;
  /** Valid values are true and false. False is the default if unspecified. If true, recursively delete all entities within the deleted space(s) regardless whether any have delete gates. */
  recursiveForce?: string;
};
export type BulkPatchSpacesApiResponse = /** status 200 OK */
  | SpaceCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ SpaceCreateOrUpdateResponseRead[];
export type BulkPatchSpacesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Space include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Space are AttributeFilterID, AttributeIDs, OrganizationID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** If true, re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AttributeFilterID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    TriggerFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereAttribute?: string | null;
    WhereTrigger?: string | null;
  };
};
export type BulkCreateSpacesApiResponse = /** status 200 OK */
  | SpaceCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ SpaceCreateOrUpdateResponseRead[];
export type BulkCreateSpacesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Space include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Space are AttributeFilterID, AttributeIDs, OrganizationID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Space names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned Space labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AttributeFilterID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    TriggerFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereAttribute?: string | null;
    WhereTrigger?: string | null;
  };
};
export type BulkDeleteAttributesApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteAttributesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Attribute list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Attribute).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Attribute include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllAttributesApiResponse = /** status 200 OK */ ExtendedAttributeRead[];
export type ListAllAttributesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Attribute list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Attribute).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Attribute include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, AttributeID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchAttributesApiResponse = /** status 200 OK */
  | AttributeCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ AttributeCreateOrUpdateResponseRead[];
export type BulkPatchAttributesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Attribute list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Attribute).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Attribute include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    DataType?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Parameters?: (object | null)[] | null;
    ResourceTypePaths?: (object | null)[] | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkCreateAttributesApiResponse = /** status 200 OK */
  | AttributeCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ AttributeCreateOrUpdateResponseRead[];
export type BulkCreateAttributesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Attribute list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Attribute).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Attribute include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Attribute names */
  namePrefixes?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning attributes
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    DataType?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Parameters?: (object | null)[] | null;
    ResourceTypePaths?: (object | null)[] | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkDeleteBridgeWorkersApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteBridgeWorkersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of BridgeWorkers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on BridgeWorker: Annotations, BridgeWorkerID, Condition, CreatedAt, DisplayName, IPAddress, Labels, LastMessage, LastSeenAt, OrgRole, OrganizationID, Permissions, Slug, SpaceID, UpdatedAt, UserID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the BridgeWorker list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (BridgeWorker).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for BridgeWorker include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for BridgeWorker are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllBridgeWorkersApiResponse = /** status 200 OK */ ExtendedBridgeWorkerRead[];
export type ListAllBridgeWorkersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of BridgeWorkers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on BridgeWorker: Annotations, BridgeWorkerID, Condition, CreatedAt, DisplayName, IPAddress, Labels, LastMessage, LastSeenAt, OrgRole, OrganizationID, Permissions, Slug, SpaceID, UpdatedAt, UserID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for BridgeWorker include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for BridgeWorker are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, BridgeWorkerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Include summary information in the response */
  summary?: boolean;
};
export type BulkPatchBridgeWorkersApiResponse = /** status 200 OK */
  | BridgeWorkerCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ BridgeWorkerCreateOrUpdateResponseRead[];
export type BulkPatchBridgeWorkersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of BridgeWorkers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on BridgeWorker: Annotations, BridgeWorkerID, Condition, CreatedAt, DisplayName, IPAddress, Labels, LastMessage, LastSeenAt, OrgRole, OrganizationID, Permissions, Slug, SpaceID, UpdatedAt, UserID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the BridgeWorker list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (BridgeWorker).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for BridgeWorker include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for BridgeWorker are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    Condition?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    OrgRole?: string | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    ProvidedInfo?: object | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type CreateActionResultApiResponse = /** status 200 OK */ string;
export type CreateActionResultApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  actionResult: ActionResult;
};
export type GetSelfApiResponse =
  /** status 200 BridgeWorker represents a bridge worker in ConfigHub.
A bridge worker is a worker program that connects ConfigHub to external systems and targets.
It acts as a bridge between ConfigHub and the infrastructure where configurations need
to be applied. Bridge workers are responsible for executing configuration changes on
remote targets and reporting status back to ConfigHub.
When starting a bridge worker program, both the BridgeWorkerID and Secret are
required for authentication with the ConfigHub server. These credentials allow the
bridge worker to establish a secure connection and receive configuration actions. */ BridgeWorkerRead;
export type GetSelfApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type ListQueuedOperationsApiResponse = /** status 200 OK */ QueuedOperation[];
export type ListQueuedOperationsApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of QueuedOperations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on QueuedOperation: Action, BridgeWorkerID, CreatedAt, DriftReconciliationMode, DryRun, OrganizationID, QueuedOperationID, RevisionNum, SpaceID, Status, TargetID, UnitActionNum, UnitID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the QueuedOperation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (QueuedOperation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for QueuedOperation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type GetQueuedOperationApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ QueuedOperation;
export type GetQueuedOperationApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  /** Unique identifier for a queued_operation_id */
  queuedOperationId: string;
};
export type StreamBridgeWorkerApiResponse = /** status 200 OK */ EventMessage;
export type StreamBridgeWorkerApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type UserCreateActionResultApiResponse = /** status 200 OK */ string;
export type UserCreateActionResultApiArg = {
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  actionResult: ActionResult;
};
export type BulkDeleteChangeSetsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteChangeSetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of ChangeSets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on ChangeSet: Annotations, ChangeSetID, CreatedAt, DeleteGates, Description, DisplayName, EndTagID, Labels, OrganizationID, Slug, SpaceID, StartTagID, State, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the ChangeSet list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (ChangeSet).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for ChangeSet include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllChangeSetsApiResponse = /** status 200 OK */ ExtendedChangeSetRead[];
export type ListAllChangeSetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of ChangeSets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on ChangeSet: Annotations, ChangeSetID, CreatedAt, DeleteGates, Description, DisplayName, EndTagID, Labels, OrganizationID, Slug, SpaceID, StartTagID, State, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the ChangeSet list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (ChangeSet).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for ChangeSet include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ChangeSetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchChangeSetsApiResponse = /** status 200 OK */
  | ChangeSetCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ ChangeSetCreateOrUpdateResponseRead[];
export type BulkPatchChangeSetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of ChangeSets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on ChangeSet: Annotations, ChangeSetID, CreatedAt, DeleteGates, Description, DisplayName, EndTagID, Labels, OrganizationID, Slug, SpaceID, StartTagID, State, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the ChangeSet list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (ChangeSet).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for ChangeSet include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkCreateChangeSetsApiResponse = /** status 200 OK */
  | ChangeSetCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ ChangeSetCreateOrUpdateResponseRead[];
export type BulkCreateChangeSetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of ChangeSets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on ChangeSet: Annotations, ChangeSetID, CreatedAt, DeleteGates, Description, DisplayName, EndTagID, Labels, OrganizationID, Slug, SpaceID, StartTagID, State, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the ChangeSet list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (ChangeSet).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for ChangeSet include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned ChangeSet names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned ChangeSet labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning changesets
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkDeleteFiltersApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteFiltersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Filters returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Filter: Annotations, CreatedAt, DeleteGates, DisplayName, FilterID, From, FromSpaceID, Hash, Labels, OrganizationID, ResourceType, Slug, SpaceID, UpdatedAt, Where, WhereData.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Filter list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Filter).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Filter include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllFiltersApiResponse = /** status 200 OK */ ExtendedFilterRead[];
export type ListAllFiltersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Filters returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Filter: Annotations, CreatedAt, DeleteGates, DisplayName, FilterID, From, FromSpaceID, Hash, Labels, OrganizationID, ResourceType, Slug, SpaceID, UpdatedAt, Where, WhereData.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Filter list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Filter).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Filter include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, FilterID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Entity type to filter for (e.g., Unit, Space). Must be specified together with 'id' parameter. */
  entity?: string;
  /** Entity ID to filter for. Must be specified together with 'entity' parameter. */
  id?: string;
};
export type BulkPatchFiltersApiResponse = /** status 200 OK */
  | FilterCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ FilterCreateOrUpdateResponseRead[];
export type BulkPatchFiltersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Filters returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Filter: Annotations, CreatedAt, DeleteGates, DisplayName, FilterID, From, FromSpaceID, Hash, Labels, OrganizationID, ResourceType, Slug, SpaceID, UpdatedAt, Where, WhereData.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Filter list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Filter).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Filter include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    From?: string | null;
    FromSpaceID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    ResourceType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Where?: string | null;
    WhereData?: string | null;
  };
};
export type BulkCreateFiltersApiResponse = /** status 200 OK */
  | FilterCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ FilterCreateOrUpdateResponseRead[];
export type BulkCreateFiltersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Filters returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Filter: Annotations, CreatedAt, DeleteGates, DisplayName, FilterID, From, FromSpaceID, Hash, Labels, OrganizationID, ResourceType, Slug, SpaceID, UpdatedAt, Where, WhereData.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Filter list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Filter).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Filter include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Filter names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned Filter labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A Go-template string for clone name, use .SourceEntity to access the original entity and .Labels to access variant labels */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning filters
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    From?: string | null;
    FromSpaceID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    ResourceType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Where?: string | null;
    WhereData?: string | null;
  };
};
export type ListOrgFunctionsApiResponse = /** status 200 OK */ {
  [key: string]: {
    [key: string]: FunctionSignature;
  };
};
export type ListOrgFunctionsApiArg = {
  /** Space ID or slug whose executor to use for listing or invoking builtin functions */
  executorSpace?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of FunctionSignatures returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on FunctionSignature: AttributeName, Description, FunctionName, FunctionType, Hermetic, Idempotent, Mutating, OutputInfo.Description, OutputInfo.OutputType, OutputInfo.ResultName, RequiredParameters, ToolchainType, Validating, VarArgs.
    
    The whole string must be query-encoded. */
  where?: string;
};
export type InvokeFunctionsOnOrgApiResponse = /** status 200 OK */
  | FunctionInvocationsResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ FunctionInvocationsResponse[];
export type InvokeFunctionsOnOrgApiArg = {
  /** Space ID or slug whose executor to use for listing or invoking builtin functions */
  executorSpace?: string;
  /** Dry run mode: when true, skip updating configuration data even if it changed */
  dryRun?: string;
  /** Must match ChangeSetID of affected Units unless in dry run mode; not valid when invoked on Revisions */
  changeSetId?: string;
  /** User-defined category for the Mutation. Must be alphanumeric, at most 64 characters. The prefix 'ConfigHub' is reserved. */
  subgroup?: string;
  /** Source of additional configuration data to pass to functions that need it (e.g., vet-immutable). Supports named revision specifiers: LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum, HeadRevisionNum. Can be prefixed with 'Before:' (e.g., Before:HeadRevisionNum). May be repeated for multiple sources. */
  otherDataSource?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Resource type: Resource type to match for the desired ToolchainType, for example apps/v1/Deployment */
  resourceType?: string;
  /** Where data: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. The whole string must be query-encoded. */
  whereData?: string;
  /** Where expression to match Triggers. Matched triggers are invoked on each unit to filter by validation results. Use with triggers_passed to control whether passing or failing units are returned (default: failing). */
  whereTrigger?: string;
  /** Filter UUID (with From=Trigger). The filter's matching triggers are invoked on units to filter by validation results. Can be combined with where_trigger. */
  triggerFilter?: string;
  /** When true, return units that pass trigger validation; when false (default), return units that fail. Only applies when where_trigger or trigger_filter is specified. */
  triggersPassed?: boolean;
  /** View slug or UUID. Applies the View's column definitions to extract values for each unit. If the View has a FilterID, its filter is ANDed with other filters. The View must have Of=Unit or a Filter with From=Unit. */
  view?: string;
  functionInvocationsRequest: FunctionInvocationsRequest;
};
export type ApiInfoApiResponse =
  /** status 200 Information provided to clients by the server. */ ApiInfoRead;
export type ApiInfoApiArg = void;
export type BulkDeleteInvocationsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteInvocationsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Invocations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Invocation: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, FunctionName, Hash, InvocationID, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Invocation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Invocation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Invocation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllInvocationsApiResponse = /** status 200 OK */ ExtendedInvocationRead[];
export type ListAllInvocationsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Invocations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Invocation: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, FunctionName, Hash, InvocationID, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Invocation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Invocation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Invocation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, InvocationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchInvocationsApiResponse = /** status 200 OK */
  | InvocationCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ InvocationCreateOrUpdateResponseRead[];
export type BulkPatchInvocationsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Invocations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Invocation: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, FunctionName, Hash, InvocationID, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Invocation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Invocation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Invocation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** Function name */
    FunctionName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
  };
};
export type BulkCreateInvocationsApiResponse = /** status 200 OK */
  | InvocationCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ InvocationCreateOrUpdateResponseRead[];
export type BulkCreateInvocationsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Invocations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Invocation: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, FunctionName, Hash, InvocationID, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Invocation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Invocation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Invocation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Invocation names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned Invocation labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning invocations
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** Function name */
    FunctionName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
  };
};
export type BulkDeleteLinksApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteLinksApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    filter
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Link list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Link).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Link include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Link are FromUnitID, OrganizationID, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type SearchListLinksApiResponse = /** status 200 OK */ ExtendedLinkRead[];
export type SearchListLinksApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Link list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Link).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Link include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Link are FromUnitID, OrganizationID, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, LinkID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchLinksApiResponse = /** status 200 OK */
  | LinkCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ LinkCreateOrUpdateResponseRead[];
export type BulkPatchLinksApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    filter
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Link list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Link).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Link include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Link are FromUnitID, OrganizationID, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Swap the FromUnit and ToUnit directions of the links */
  reverse?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AutoUpdate?: boolean | null;
    Bindings?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    DownstreamLastMergedRevisionNum?: number | null;
    DownstreamPaths?: (object | null)[] | null;
    DownstreamSetters?: (object | null)[] | null;
    FromUnitID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    MergeDisableSubtraction?: boolean | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToSpaceID?: string | null;
    ToUnitID?: string | null;
    TransformInvocationID?: string | null;
    UpdateType?: string | null;
    UpstreamGetters?: (object | null)[] | null;
    UpstreamLastMergedRevisionNum?: number | null;
    UpstreamPaths?: (object | null)[] | null;
    UseLiveState?: boolean | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereMutation?: string | null;
    WhereResource?: string | null;
  };
};
export type BulkCreateLinksApiResponse = /** status 200 OK */
  | LinkCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ LinkCreateOrUpdateResponseRead[];
export type BulkCreateLinksApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    Where expression to select source links to copy
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Link list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Link).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Swap the FromUnit and ToUnit directions of the copied links (for cross-space link reversal) */
  reverse?: boolean;
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    Where expression to find downstream UpgradeUnit links from each source link's FromUnit. Creates one copy per match. Required if reverse is not specified.
    
    The whole string must be query-encoded. */
  fromDownstreamWhere?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    Where expression to find downstream UpgradeUnit link from each source link's ToUnit. Exactly one match required. If omitted, ToUnitID/ToSpaceID are unchanged.
    
    The whole string must be query-encoded. */
  toDownstreamWhere?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AutoUpdate?: boolean | null;
    Bindings?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    DownstreamLastMergedRevisionNum?: number | null;
    DownstreamPaths?: (object | null)[] | null;
    DownstreamSetters?: (object | null)[] | null;
    FromUnitID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    MergeDisableSubtraction?: boolean | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToSpaceID?: string | null;
    ToUnitID?: string | null;
    TransformInvocationID?: string | null;
    UpdateType?: string | null;
    UpstreamGetters?: (object | null)[] | null;
    UpstreamLastMergedRevisionNum?: number | null;
    UpstreamPaths?: (object | null)[] | null;
    UseLiveState?: boolean | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereMutation?: string | null;
    WhereResource?: string | null;
  };
};
export type GetMeApiResponse =
  /** status 200 a User given membership on the Organization */ OrganizationMember;
export type GetMeApiArg = void;
export type ListOrganizationsApiResponse = /** status 200 OK */ OrganizationRead[];
export type ListOrganizationsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Organizations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Organization: Annotations, CreatedAt, DeleteGates, DisplayName, EmailDomain, ExternalID, Labels, OrganizationID, Slug, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Organization list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Organization).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Organization include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Organization.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Organization are .
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Organization.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, OrganizationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateOrganizationApiResponse =
  /** status 200 The top-level container for an organization using ConfigHub. */ OrganizationRead;
export type CreateOrganizationApiArg = {
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  organization: Organization;
};
export type DeleteOrganizationApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteOrganizationApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
};
export type GetOrganizationApiResponse =
  /** status 200 The top-level container for an organization using ConfigHub. */ OrganizationRead;
export type GetOrganizationApiArg = {
  /** Include clause for expanding related entities in the response for Organization.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Organization are .
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Organization.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, OrganizationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a organization_id */
  organizationId: string;
};
export type UpdateOrganizationApiResponse =
  /** status 200 The top-level container for an organization using ConfigHub. */ OrganizationRead;
export type UpdateOrganizationApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
  organization: Organization;
};
export type ListOrganizationMembersApiResponse = /** status 200 OK */ OrganizationMember[];
export type ListOrganizationMembersApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of OrganizationMembers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on OrganizationMember: DisplayName, ExternalID, Slug, UserID, Username.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the OrganizationMember list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (OrganizationMember).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for OrganizationMember include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type CreateOrganizationMemberApiResponse =
  /** status 200 a User given membership on the Organization */ OrganizationMember;
export type CreateOrganizationMemberApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
  organizationMember: OrganizationMember;
};
export type DeleteOrganizationMemberApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteOrganizationMemberApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
  /** Unique identifier for a organization_member_id */
  organizationMemberId: string;
};
export type GetOrganizationMemberApiResponse =
  /** status 200 a User given membership on the Organization */ OrganizationMember;
export type GetOrganizationMemberApiArg = {
  /** Unique identifier for a organization_id */
  organizationId: string;
  /** Unique identifier for a organization_member_id */
  organizationMemberId: string;
};
export type ListAllRevisionsApiResponse = /** status 200 OK */ ExtendedRevisionRead[];
export type ListAllRevisionsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Revisions returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Revision: ApplyGates, ApplyWarnings, ApprovedBy, ChangeSetID, CreatedAt, DataHash, Description, LiveAt, OrganizationID, RevisionID, RevisionNum, Source, SpaceID, Tags, UnitID, UpdatedAt, UserAgent, UserID.
    
    To list tagged Revisions use `Tags ? '<tag-id>'`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Revision list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Revision).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Revision include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Revision are ChangeSetID, OrganizationID, SpaceID, Tags, UnitID, UserID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, RevisionID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type ListSpacesApiResponse = /** status 200 OK */ ExtendedSpaceRead[];
export type ListSpacesApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Space include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Space are AttributeFilterID, AttributeIDs, OrganizationID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, SpaceID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Return summarized entity data */
  summary?: boolean;
};
export type CreateSpaceApiResponse =
  /** status 200 The logical container for most entities in ConfigHub. Namespaces triggers, units, targets, workers, and other entities. */ SpaceRead;
export type CreateSpaceApiArg = {
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  space: Space;
};
export type DeleteSpaceApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteSpaceApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Valid values are true and false. False is the default if unspecified. If true, recursively delete all entities within the deleted space(s) so long as none have delete gates. */
  recursive?: string;
  /** Valid values are true and false. False is the default if unspecified. If true, recursively delete all entities within the deleted space(s) regardless whether any have delete gates. */
  recursiveForce?: string;
};
export type GetSpaceApiResponse = /** status 200 OK */ ExtendedSpaceRead;
export type GetSpaceApiArg = {
  /** Include clause for expanding related entities in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Space are AttributeFilterID, AttributeIDs, OrganizationID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Space.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, SpaceID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Return summarized entity data */
  summary?: boolean;
};
export type PatchSpaceApiResponse =
  /** status 200 The logical container for most entities in ConfigHub. Namespaces triggers, units, targets, workers, and other entities. */ SpaceRead;
export type PatchSpaceApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** If true, re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AttributeFilterID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    TriggerFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereAttribute?: string | null;
    WhereTrigger?: string | null;
  };
};
export type UpdateSpaceApiResponse =
  /** status 200 The logical container for most entities in ConfigHub. Namespaces triggers, units, targets, workers, and other entities. */ SpaceRead;
export type UpdateSpaceApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** If true, re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  space: Space;
};
export type ListAttributesApiResponse = /** status 200 OK */ ExtendedAttributeRead[];
export type ListAttributesApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Attribute list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Attribute).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Attribute include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, AttributeID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateAttributeApiResponse =
  /** status 200 Defines a dynamic configuration attribute that registers getter and setter functions
and their associated paths in a Space's FunctionExecutor. Attributes enable per-Space
customization of the function registry by specifying a set of paths within a resource
type that can be read and written using generated get-<slug> and set-<slug> functions. */ AttributeRead;
export type CreateAttributeApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  attribute: Attribute;
};
export type DeleteAttributeApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteAttributeApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a attribute_id */
  attributeId: string;
};
export type GetAttributeApiResponse = /** status 200 OK */ ExtendedAttributeRead;
export type GetAttributeApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Attribute are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Attribute.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, AttributeID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a attribute_id */
  attributeId: string;
};
export type PatchAttributeApiResponse =
  /** status 200 Defines a dynamic configuration attribute that registers getter and setter functions
and their associated paths in a Space's FunctionExecutor. Attributes enable per-Space
customization of the function registry by specifying a set of paths within a resource
type that can be read and written using generated get-<slug> and set-<slug> functions. */ AttributeRead;
export type PatchAttributeApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a attribute_id */
  attributeId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    DataType?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Parameters?: (object | null)[] | null;
    ResourceTypePaths?: (object | null)[] | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateAttributeApiResponse =
  /** status 200 Defines a dynamic configuration attribute that registers getter and setter functions
and their associated paths in a Space's FunctionExecutor. Attributes enable per-Space
customization of the function registry by specifying a set of paths within a resource
type that can be read and written using generated get-<slug> and set-<slug> functions. */ AttributeRead;
export type UpdateAttributeApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a attribute_id */
  attributeId: string;
  attribute: Attribute;
};
export type ListBridgeWorkersApiResponse = /** status 200 OK */ ExtendedBridgeWorkerRead[];
export type ListBridgeWorkersApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of BridgeWorkers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on BridgeWorker: Annotations, BridgeWorkerID, Condition, CreatedAt, DisplayName, IPAddress, Labels, LastMessage, LastSeenAt, OrgRole, OrganizationID, Permissions, Slug, SpaceID, UpdatedAt, UserID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the BridgeWorker list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (BridgeWorker).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for BridgeWorker include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for BridgeWorker are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, BridgeWorkerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateBridgeWorkerApiResponse =
  /** status 200 BridgeWorker represents a bridge worker in ConfigHub.
A bridge worker is a worker program that connects ConfigHub to external systems and targets.
It acts as a bridge between ConfigHub and the infrastructure where configurations need
to be applied. Bridge workers are responsible for executing configuration changes on
remote targets and reporting status back to ConfigHub.
When starting a bridge worker program, both the BridgeWorkerID and Secret are
required for authentication with the ConfigHub server. These credentials allow the
bridge worker to establish a secure connection and receive configuration actions. */ BridgeWorkerRead;
export type CreateBridgeWorkerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  bridgeWorker: BridgeWorker;
};
export type DeleteBridgeWorkerApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteBridgeWorkerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type GetBridgeWorkerApiResponse = /** status 200 OK */ ExtendedBridgeWorkerRead;
export type GetBridgeWorkerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for BridgeWorker are OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for BridgeWorker.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, BridgeWorkerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type PatchBridgeWorkerApiResponse =
  /** status 200 BridgeWorker represents a bridge worker in ConfigHub.
A bridge worker is a worker program that connects ConfigHub to external systems and targets.
It acts as a bridge between ConfigHub and the infrastructure where configurations need
to be applied. Bridge workers are responsible for executing configuration changes on
remote targets and reporting status back to ConfigHub.
When starting a bridge worker program, both the BridgeWorkerID and Secret are
required for authentication with the ConfigHub server. These credentials allow the
bridge worker to establish a secure connection and receive configuration actions. */ BridgeWorkerRead;
export type PatchBridgeWorkerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    Condition?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    OrgRole?: string | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    ProvidedInfo?: object | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateBridgeWorkerApiResponse =
  /** status 200 BridgeWorker represents a bridge worker in ConfigHub.
A bridge worker is a worker program that connects ConfigHub to external systems and targets.
It acts as a bridge between ConfigHub and the infrastructure where configurations need
to be applied. Bridge workers are responsible for executing configuration changes on
remote targets and reporting status back to ConfigHub.
When starting a bridge worker program, both the BridgeWorkerID and Secret are
required for authentication with the ConfigHub server. These credentials allow the
bridge worker to establish a secure connection and receive configuration actions. */ BridgeWorkerRead;
export type UpdateBridgeWorkerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  bridgeWorker: BridgeWorker;
};
export type ListBridgeWorkerFunctionsApiResponse = /** status 200 OK */ {
  [key: string]: {
    [key: string]: FunctionSignature;
  };
};
export type ListBridgeWorkerFunctionsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type ListBridgeWorkerStatusesApiResponse = /** status 200 OK */ BridgeWorkerStatus[];
export type ListBridgeWorkerStatusesApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
};
export type GetBridgeWorkerStatusApiResponse =
  /** status 200 BridgeWorkerStatus represents the status information of a bridge worker within the system. */ BridgeWorkerStatus;
export type GetBridgeWorkerStatusApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a bridge_worker_id */
  bridgeWorkerId: string;
  /** Unique identifier for a status_id */
  statusId: string;
};
export type ListChangeSetsApiResponse = /** status 200 OK */ ExtendedChangeSetRead[];
export type ListChangeSetsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of ChangeSets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on ChangeSet: Annotations, ChangeSetID, CreatedAt, DeleteGates, Description, DisplayName, EndTagID, Labels, OrganizationID, Slug, SpaceID, StartTagID, State, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the ChangeSet list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (ChangeSet).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for ChangeSet include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ChangeSetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateChangeSetApiResponse =
  /** status 200 Defines an entity changeset. */ ChangeSetRead;
export type CreateChangeSetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  changeSet: ChangeSet;
};
export type DeleteChangeSetApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteChangeSetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a change_set_id */
  changeSetId: string;
};
export type GetChangeSetApiResponse = /** status 200 OK */ ExtendedChangeSetRead;
export type GetChangeSetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for ChangeSet are EndTagID, OrganizationID, SpaceID, StartTagID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for ChangeSet.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ChangeSetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a change_set_id */
  changeSetId: string;
};
export type PatchChangeSetApiResponse =
  /** status 200 Defines an entity changeset. */ ChangeSetRead;
export type PatchChangeSetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a change_set_id */
  changeSetId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateChangeSetApiResponse =
  /** status 200 Defines an entity changeset. */ ChangeSetRead;
export type UpdateChangeSetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a change_set_id */
  changeSetId: string;
  changeSet: ChangeSet;
};
export type ListFiltersApiResponse = /** status 200 OK */ ExtendedFilterRead[];
export type ListFiltersApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Filters returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Filter: Annotations, CreatedAt, DeleteGates, DisplayName, FilterID, From, FromSpaceID, Hash, Labels, OrganizationID, ResourceType, Slug, SpaceID, UpdatedAt, Where, WhereData.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Filter list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Filter).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Filter include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, FilterID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Entity type to filter for (e.g., Unit, Space). Must be specified together with 'id' parameter. */
  entity?: string;
  /** Entity ID to filter for. Must be specified together with 'entity' parameter. */
  id?: string;
};
export type CreateFilterApiResponse = /** status 200 Defines an entity filter. */ FilterRead;
export type CreateFilterApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  filter: Filter;
};
export type DeleteFilterApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteFilterApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a filter_id */
  filterId: string;
};
export type GetFilterApiResponse = /** status 200 OK */ ExtendedFilterRead;
export type GetFilterApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Filter are FromSpaceID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Filter.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, FilterID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a filter_id */
  filterId: string;
};
export type PatchFilterApiResponse = /** status 200 Defines an entity filter. */ FilterRead;
export type PatchFilterApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a filter_id */
  filterId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    From?: string | null;
    FromSpaceID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    ResourceType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Where?: string | null;
    WhereData?: string | null;
  };
};
export type UpdateFilterApiResponse = /** status 200 Defines an entity filter. */ FilterRead;
export type UpdateFilterApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a filter_id */
  filterId: string;
  filter: Filter;
};
export type ListFunctionsApiResponse = /** status 200 OK */ {
  [key: string]: {
    [key: string]: FunctionSignature;
  };
};
export type ListFunctionsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Type of entity used to identify the worker whose functions should be listed: unit, target, or worker */
  entity?: string;
  /** ID of the entity used to identify the worker whose functions should be listed */
  id?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of FunctionSignatures returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on FunctionSignature: AttributeName, Description, FunctionName, FunctionType, Hermetic, Idempotent, Mutating, OutputInfo.Description, OutputInfo.OutputType, OutputInfo.ResultName, RequiredParameters, ToolchainType, Validating, VarArgs.
    
    The whole string must be query-encoded. */
  where?: string;
};
export type InvokeFunctionsApiResponse = /** status 200 OK */
  | FunctionInvocationsResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ FunctionInvocationsResponse[];
export type InvokeFunctionsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unit ID of the Revision to invoke functions on */
  unitId?: string;
  /** Revision ID to invoke functions on instead of units */
  revisionId?: string;
  /** Dry run mode: when true, skip updating configuration data even if it changed */
  dryRun?: string;
  /** Must match ChangeSetID of affected Units unless in dry run mode; not valid when invoked on Revisions */
  changeSetId?: string;
  /** User-defined category for the Mutation. Must be alphanumeric, at most 64 characters. The prefix 'ConfigHub' is reserved. */
  subgroup?: string;
  /** Source of additional configuration data to pass to functions that need it (e.g., vet-immutable). Supports named revision specifiers: LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum, HeadRevisionNum. Can be prefixed with 'Before:' (e.g., Before:HeadRevisionNum). May be repeated for multiple sources. */
  otherDataSource?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Resource type: Resource type to match for the desired ToolchainType, for example apps/v1/Deployment */
  resourceType?: string;
  /** Where data: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. The whole string must be query-encoded. */
  whereData?: string;
  /** Where expression to match Triggers. Matched triggers are invoked on each unit to filter by validation results. Use with triggers_passed to control whether passing or failing units are returned (default: failing). */
  whereTrigger?: string;
  /** Filter UUID (with From=Trigger). The filter's matching triggers are invoked on units to filter by validation results. Can be combined with where_trigger. */
  triggerFilter?: string;
  /** When true, return units that pass trigger validation; when false (default), return units that fail. Only applies when where_trigger or trigger_filter is specified. */
  triggersPassed?: boolean;
  /** View slug or UUID. Applies the View's column definitions to extract values for each unit. If the View has a FilterID, its filter is ANDed with other filters. The View must have Of=Unit or a Filter with From=Unit. */
  view?: string;
  functionInvocationsRequest: FunctionInvocationsRequest;
};
export type ListInvocationsApiResponse = /** status 200 OK */ ExtendedInvocationRead[];
export type ListInvocationsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Invocations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Invocation: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, FunctionName, Hash, InvocationID, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Invocation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Invocation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Invocation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, InvocationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateInvocationApiResponse =
  /** status 200 Defines a function invocation. */ InvocationRead;
export type CreateInvocationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  invocation: Invocation;
};
export type DeleteInvocationApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteInvocationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a invocation_id */
  invocationId: string;
};
export type GetInvocationApiResponse = /** status 200 OK */ ExtendedInvocationRead;
export type GetInvocationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Invocation are BridgeWorkerID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Invocation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, InvocationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a invocation_id */
  invocationId: string;
};
export type PatchInvocationApiResponse =
  /** status 200 Defines a function invocation. */ InvocationRead;
export type PatchInvocationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a invocation_id */
  invocationId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** Function name */
    FunctionName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
  };
};
export type UpdateInvocationApiResponse =
  /** status 200 Defines a function invocation. */ InvocationRead;
export type UpdateInvocationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a invocation_id */
  invocationId: string;
  invocation: Invocation;
};
export type ListLinksApiResponse = /** status 200 OK */ ExtendedLinkRead[];
export type ListLinksApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Link list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Link).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Link include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Link are FromUnitID, OrganizationID, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, LinkID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateLinkApiResponse =
  /** status 200 Link connects two config Units in a dependency / producer-consumer relationship.
A Link indicates that selected config data from the upstream To Unit (the producer)
should be propagated to the downstream From Unit (the consumer).
Links must be created in the same Space as the From Unit.
They also imply an ordering when Applied or Destroyed as a group. */ LinkRead;
export type CreateLinkApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  link: Link;
};
export type DeleteLinkApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteLinkApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a link_id */
  linkId: string;
};
export type GetLinkApiResponse = /** status 200 OK */ ExtendedLinkRead;
export type GetLinkApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Link are FromUnitID, OrganizationID, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Link.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, LinkID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a link_id */
  linkId: string;
};
export type PatchLinkApiResponse =
  /** status 200 Link connects two config Units in a dependency / producer-consumer relationship.
A Link indicates that selected config data from the upstream To Unit (the producer)
should be propagated to the downstream From Unit (the consumer).
Links must be created in the same Space as the From Unit.
They also imply an ordering when Applied or Destroyed as a group. */ LinkRead;
export type PatchLinkApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a link_id */
  linkId: string;
  /** Swap the FromUnit and ToUnit directions of the link */
  reverse?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    AutoUpdate?: boolean | null;
    Bindings?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    DownstreamLastMergedRevisionNum?: number | null;
    DownstreamPaths?: (object | null)[] | null;
    DownstreamSetters?: (object | null)[] | null;
    FromUnitID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    MergeDisableSubtraction?: boolean | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToSpaceID?: string | null;
    ToUnitID?: string | null;
    TransformInvocationID?: string | null;
    UpdateType?: string | null;
    UpstreamGetters?: (object | null)[] | null;
    UpstreamLastMergedRevisionNum?: number | null;
    UpstreamPaths?: (object | null)[] | null;
    UseLiveState?: boolean | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereMutation?: string | null;
    WhereResource?: string | null;
  };
};
export type UpdateLinkApiResponse =
  /** status 200 Link connects two config Units in a dependency / producer-consumer relationship.
A Link indicates that selected config data from the upstream To Unit (the producer)
should be propagated to the downstream From Unit (the consumer).
Links must be created in the same Space as the From Unit.
They also imply an ordering when Applied or Destroyed as a group. */ LinkRead;
export type UpdateLinkApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a link_id */
  linkId: string;
  link: Link;
};
export type ListTagsApiResponse = /** status 200 OK */ ExtendedTagRead[];
export type ListTagsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Tags returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Tag: Annotations, ChangeSetID, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Slug, SpaceID, TagID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Tag list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Tag).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Tag include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TagID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateTagApiResponse =
  /** status 200 Defines a Tag that can be used to identify a set of Revisions across Units. */ TagRead;
export type CreateTagApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  tag: Tag;
};
export type DeleteTagApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteTagApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a tag_id */
  tagId: string;
};
export type GetTagApiResponse = /** status 200 OK */ ExtendedTagRead;
export type GetTagApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TagID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a tag_id */
  tagId: string;
};
export type PatchTagApiResponse =
  /** status 200 Defines a Tag that can be used to identify a set of Revisions across Units. */ TagRead;
export type PatchTagApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a tag_id */
  tagId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateTagApiResponse =
  /** status 200 Defines a Tag that can be used to identify a set of Revisions across Units. */ TagRead;
export type UpdateTagApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a tag_id */
  tagId: string;
  tag: Tag;
};
export type ListTargetsApiResponse = /** status 200 OK */ ExtendedTargetRead[];
export type ListTargetsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Targets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Target: Annotations, BridgeHandle, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, Facts, Labels, LiveStateType, Options, OrganizationID, Permissions, ProviderType, Slug, SpaceID, TargetID, ToolchainType, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Target list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Target).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Target include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Target are BridgeWorkerID, OrganizationID, SpaceID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TargetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateTargetApiResponse =
  /** status 200 Target represents a deployment target in ConfigHub. It defines where configuration should be applied, including the toolchain type (e.g., Kubernetes/YAML, AppConfig/Properties, AppConfig/YAML, AppConfig/TOML, AppConfig/INI, AppConfig/JSON, AppConfig/Env, AppConfig/Text) and provider (e.g., ArgoCDOCI, FluxOCI). Each Target is associated with a specific BridgeWorker that handles the actual deployment actions (e.g. Apply, Destroy). */ TargetRead;
export type CreateTargetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  target: Target;
};
export type DeleteTargetApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteTargetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a target_id */
  targetId: string;
};
export type GetTargetApiResponse = /** status 200 OK */ ExtendedTargetRead;
export type GetTargetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Target are BridgeWorkerID, OrganizationID, SpaceID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TargetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a target_id */
  targetId: string;
};
export type PatchTargetApiResponse =
  /** status 200 Target represents a deployment target in ConfigHub. It defines where configuration should be applied, including the toolchain type (e.g., Kubernetes/YAML, AppConfig/Properties, AppConfig/YAML, AppConfig/TOML, AppConfig/INI, AppConfig/JSON, AppConfig/Env, AppConfig/Text) and provider (e.g., ArgoCDOCI, FluxOCI). Each Target is associated with a specific BridgeWorker that handles the actual deployment actions (e.g. Apply, Destroy). */ TargetRead;
export type PatchTargetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a target_id */
  targetId: string;
  /** Re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    BridgeHandle?: string | null;
    BridgeWorkerID?: string | null;
    ConfigTypes?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    Facts?: {
      [key: string]: string | null;
    } | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    LiveStateType?: string | null;
    Options?: {
      [key: string]: string | null;
    } | null;
    Parameters?: string | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    ProviderType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    TriggerFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereTrigger?: string | null;
  };
};
export type UpdateTargetApiResponse =
  /** status 200 Target represents a deployment target in ConfigHub. It defines where configuration should be applied, including the toolchain type (e.g., Kubernetes/YAML, AppConfig/Properties, AppConfig/YAML, AppConfig/TOML, AppConfig/INI, AppConfig/JSON, AppConfig/Env, AppConfig/Text) and provider (e.g., ArgoCDOCI, FluxOCI). Each Target is associated with a specific BridgeWorker that handles the actual deployment actions (e.g. Apply, Destroy). */ TargetRead;
export type UpdateTargetApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a target_id */
  targetId: string;
  /** Re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  target: Target;
};
export type ListTriggersApiResponse = /** status 200 OK */ ExtendedTriggerRead[];
export type ListTriggersApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Trigger list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Trigger).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Trigger include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TriggerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateTriggerApiResponse =
  /** status 200 Defines an automated function invocation that executes in response to specific
Unit lifecycle events in ConfigHub. Triggers can be used to implement validation rules,
automated transformations, or other custom logic that should run when configuration
changes occur. Each Trigger is associated with a specific Space and can be configured
to execute on events.

Triggers can be either validating (checking configuration validity without modifying it)
or mutating (making changes to the configuration). They can be disabled, and validating
triggers can be set to Warn mode to produce non-blocking ApplyWarnings instead of ApplyGates. */ TriggerRead;
export type CreateTriggerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  trigger: Trigger;
};
export type DeleteTriggerApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteTriggerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a trigger_id */
  triggerId: string;
};
export type GetTriggerApiResponse = /** status 200 OK */ ExtendedTriggerRead;
export type GetTriggerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TriggerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a trigger_id */
  triggerId: string;
};
export type PatchTriggerApiResponse =
  /** status 200 Defines an automated function invocation that executes in response to specific
Unit lifecycle events in ConfigHub. Triggers can be used to implement validation rules,
automated transformations, or other custom logic that should run when configuration
changes occur. Each Trigger is associated with a specific Space and can be configured
to execute on events.

Triggers can be either validating (checking configuration validity without modifying it)
or mutating (making changes to the configuration). They can be disabled, and validating
triggers can be set to Warn mode to produce non-blocking ApplyWarnings instead of ApplyGates. */ TriggerRead;
export type PatchTriggerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a trigger_id */
  triggerId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    Disabled?: boolean | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    Event?: string | null;
    FailOpenAfter?: number | null;
    /** Function name */
    FunctionName?: string | null;
    InvocationID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    OtherDataSource?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    UnitFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Warn?: boolean | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
    WhereUnit?: string | null;
  };
};
export type UpdateTriggerApiResponse =
  /** status 200 Defines an automated function invocation that executes in response to specific
Unit lifecycle events in ConfigHub. Triggers can be used to implement validation rules,
automated transformations, or other custom logic that should run when configuration
changes occur. Each Trigger is associated with a specific Space and can be configured
to execute on events.

Triggers can be either validating (checking configuration validity without modifying it)
or mutating (making changes to the configuration). They can be disabled, and validating
triggers can be set to Warn mode to produce non-blocking ApplyWarnings instead of ApplyGates. */ TriggerRead;
export type UpdateTriggerApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a trigger_id */
  triggerId: string;
  trigger: Trigger;
};
export type ListUnitsApiResponse = /** status 200 OK */ ExtendedUnitRead[];
export type ListUnitsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, UnitID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Resource type: Resource type to match for the desired ToolchainType, for example apps/v1/Deployment */
  resourceType?: string;
  /** Where data: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. The whole string must be query-encoded. */
  whereData?: string;
  /** Where expression to match Triggers. Matched triggers are invoked on each unit to filter by validation results. Use with triggers_passed to control whether passing or failing units are returned (default: failing). */
  whereTrigger?: string;
  /** Filter UUID (with From=Trigger). The filter's matching triggers are invoked on units to filter by validation results. Can be combined with where_trigger. */
  triggerFilter?: string;
  /** When true, return units that pass trigger validation; when false (default), return units that fail. Only applies when where_trigger or trigger_filter is specified. */
  triggersPassed?: boolean;
  /** View slug or UUID. Applies the View's column definitions to extract values for each unit. If the View has a FilterID, its filter is ANDed with other filters. The View must have Of=Unit or a Filter with From=Unit. */
  view?: string;
};
export type CreateUnitApiResponse =
  /** status 200 Unit is the core unit of operation in ConfigHub. It contains a blob of configuration Data
of a single supported Toolchain Type (configuration format). This blob is typically a text document
that contains a collection of Kubernetes or infrastructure resources, or an application configuration
file. Applying / deploying or destroying the configuration happens as a single *transaction*
from ConfigHub's perspective. In reality, it is most often a multi-step workflow performed by
the underlying configuration / deployment tool. The resources must belong to a single
infrastructure provider and the actuation mechanism must be able to resolve references and
ordering dependencies among the resources within the document. For example, if one resource
needs to be fully provisioned to provide input to another resource, then the actuation code is
responsible for handling this. Revisions store historical copies of the configuration data.
Configuration data can be restored from prior Revisions. Units can also be cloned to create
new variants of a configuration. */ UnitRead;
export type CreateUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a upstream_space_id */
  upstreamSpaceId?: string;
  /** Unique identifier for a upstream_unit_id */
  upstreamUnitId?: string;
  /** Identifier of the external source. Sets the source type to MergeExternal and appends the source name to the change description. */
  mergeExternalSource?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  unit: Unit;
};
export type DeleteUnitApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type GetUnitApiResponse =
  /** status 200 Unit with capability to extend additional related entities. */ ExtendedUnitRead;
export type GetUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, UnitID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type PatchUnitApiResponse =
  /** status 200 Unit is the core unit of operation in ConfigHub. It contains a blob of configuration Data
of a single supported Toolchain Type (configuration format). This blob is typically a text document
that contains a collection of Kubernetes or infrastructure resources, or an application configuration
file. Applying / deploying or destroying the configuration happens as a single *transaction*
from ConfigHub's perspective. In reality, it is most often a multi-step workflow performed by
the underlying configuration / deployment tool. The resources must belong to a single
infrastructure provider and the actuation mechanism must be able to resolve references and
ordering dependencies among the resources within the document. For example, if one resource
needs to be fully provisioned to provide input to another resource, then the actuation code is
responsible for handling this. Revisions store historical copies of the configuration data.
Configuration data can be restored from prior Revisions. Units can also be cloned to create
new variants of a configuration. */ UnitRead;
export type PatchUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Unique identifier for a revision_id */
  revisionId?: string;
  /** Dry run mode: return changed unit(s) but don't update configuration data */
  dryRun?: boolean;
  /** Upgrade the unit to the latest version of its upstream unit */
  upgrade?: boolean;
  /** Restore revision source. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  restore?: string;
  /** Resolve specified non-automatically resolved link from this (downstream) Unit to another (upstream) Unit. Expects Link:uuid or Link:*. */
  resolve?: string;
  /** Merge source unit. Currently it must be a unit ID or 'Self'. */
  mergeSource?: string;
  /** Merge base revision, which provides the base configuration data of the changes to merge. With merge_source, this is a revision of the merge source unit. With merge_external_source, this is a revision of the unit being updated and overrides the default selection of the latest MergeExternal revision. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeBase?: string;
  /** Merge end revision of the merge source, which provides the final configuration of the changes to merge. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeEnd?: string;
  /** Identifier of the external source for merge-on-update. When set, computes mutations between the last MergeExternal revision and the provided data, then patches the current unit data with those mutations. */
  mergeExternalSource?: string;
  /** Disable the subtraction (override-preservation) step of upgrade and merge_source. By default (false), a cross-unit merge subtracts the target's local differences from the source patch so they survive; set true to apply the source patch without subtraction, relying on stored Mutation Predicate values to preserve overrides. Has no effect on a self merge (merge_source=Self), where subtraction is always disabled. */
  mergeDisableSubtraction?: boolean;
  /** The specified string is an expression for the purpose of filtering
    the list of Mutations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Mutation: CreatedAt, FunctionName, InvocationID, LinkID, MergeBaseRevisionNum, MergeEndRevisionNum, MergeSourceID, MutationID, MutationNum, OrganizationID, RestoredRevisionNum, RevisionID, RevisionNum, SpaceID, Subgroup, TriggerID, UnitID, UpdatedAt, UpgradedFromUpstreamRevisionNum.
    
    Used to filter which mutations are affected during merge operations.
    
    The whole string must be query-encoded. */
  whereMutation?: string;
  /** UUID of a Filter entity to apply to the Mutation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Mutation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterMutation?: string;
  /** Tag ID to add to the head revision */
  tag?: string;
  /** Must match ChangeSetID of affected Units if config Data is changed unless in dry run mode */
  changeSetId?: string;
  /** User-defined category for the Mutation. Must be alphanumeric, at most 64 characters. The prefix 'ConfigHub' is reserved. */
  subgroup?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Unique identifier for the ChangeSet to which the current Revision belongs. Optional. Units are not required to belong to ChangeSets. */
    ChangeSetID?: string | null;
    /** The full configuration data for this unit. */
    Data?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** An optional set of gates that, if any is present, will block destroy operations */
    DestroyGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** LastChangeDescription is a human-readable description of the last change. This description is copied to the new Revision when the Data is changed. */
    LastChangeDescription?: string | null;
    ProviderType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** TargetID is the identifier of the target this unit is associated with. This defines where the configuration will be applied. It must be set to a valid Target within the same Space before the Unit can be Applied, Destroyed, Imported, or Refreshed. */
    TargetID?: string | null;
    /** Bridge option values set per-Unit, merged with the Target's Options when sending to the bridge worker (Target's Options take precedence on overlap). The options must be predefined by the ConfigType in the BridgeWorker. */
    TargetOptions?: {
      [key: string]: string | null;
    } | null;
    /** ToolchainType specifies the type of toolchain for this unit. Possible values include "Kubernetes/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text", "ConfigHub/YAML". */
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateUnitApiResponse =
  /** status 200 Unit is the core unit of operation in ConfigHub. It contains a blob of configuration Data
of a single supported Toolchain Type (configuration format). This blob is typically a text document
that contains a collection of Kubernetes or infrastructure resources, or an application configuration
file. Applying / deploying or destroying the configuration happens as a single *transaction*
from ConfigHub's perspective. In reality, it is most often a multi-step workflow performed by
the underlying configuration / deployment tool. The resources must belong to a single
infrastructure provider and the actuation mechanism must be able to resolve references and
ordering dependencies among the resources within the document. For example, if one resource
needs to be fully provisioned to provide input to another resource, then the actuation code is
responsible for handling this. Revisions store historical copies of the configuration data.
Configuration data can be restored from prior Revisions. Units can also be cloned to create
new variants of a configuration. */ UnitRead;
export type UpdateUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Unique identifier for a revision_id */
  revisionId?: string;
  /** Dry run mode: return changed unit(s) but don't update configuration data */
  dryRun?: boolean;
  /** Upgrade the unit to the latest version of its upstream unit */
  upgrade?: boolean;
  /** Restore revision source. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  restore?: string;
  /** Resolve specified non-automatically resolved link from this (downstream) Unit to another (upstream) Unit. Expects Link:uuid or Link:*. */
  resolve?: string;
  /** Merge source unit. Currently it must be a unit ID or 'Self'. */
  mergeSource?: string;
  /** Merge base revision, which provides the base configuration data of the changes to merge. With merge_source, this is a revision of the merge source unit. With merge_external_source, this is a revision of the unit being updated and overrides the default selection of the latest MergeExternal revision. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeBase?: string;
  /** Merge end revision of the merge source, which provides the final configuration of the changes to merge. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeEnd?: string;
  /** Identifier of the external source for merge-on-update. When set, computes mutations between the last MergeExternal revision and the provided data, then patches the current unit data with those mutations. */
  mergeExternalSource?: string;
  /** Disable the subtraction (override-preservation) step of upgrade and merge_source. By default (false), a cross-unit merge subtracts the target's local differences from the source patch so they survive; set true to apply the source patch without subtraction, relying on stored Mutation Predicate values to preserve overrides. Has no effect on a self merge (merge_source=Self), where subtraction is always disabled. */
  mergeDisableSubtraction?: boolean;
  /** The specified string is an expression for the purpose of filtering
    the list of Mutations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Mutation: CreatedAt, FunctionName, InvocationID, LinkID, MergeBaseRevisionNum, MergeEndRevisionNum, MergeSourceID, MutationID, MutationNum, OrganizationID, RestoredRevisionNum, RevisionID, RevisionNum, SpaceID, Subgroup, TriggerID, UnitID, UpdatedAt, UpgradedFromUpstreamRevisionNum.
    
    Used to filter which mutations are affected during merge operations.
    
    The whole string must be query-encoded. */
  whereMutation?: string;
  /** UUID of a Filter entity to apply to the Mutation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Mutation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterMutation?: string;
  /** Tag ID to add to the head revision */
  tag?: string;
  /** Must match ChangeSetID of affected Units if config Data is changed unless in dry run mode */
  changeSetId?: string;
  /** User-defined category for the Mutation. Must be alphanumeric, at most 64 characters. The prefix 'ConfigHub' is reserved. */
  subgroup?: string;
  unit: Unit;
};
export type ApplyUnitApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ QueuedOperation;
export type ApplyUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Revision to apply (defaults to HeadRevisionNum). Can be a revision number, 'LiveRevisionNum', 'LastAppliedRevisionNum', 'Tag:uuid', 'ChangeSet:uuid', etc. */
  revision?: string;
  /** Dry run mode - validates which units would be applied without executing */
  dryRun?: boolean;
  /** Drift reconciliation mode. Valid values: OnDemand, ContinuousApply, ContinuousRefresh. If not specified, the current value on the Unit is used. */
  driftMode?: string;
};
export type ApproveUnitApiResponse = /** status 200 OK */ ApproveResponseRead;
export type ApproveUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Revision to approve (defaults to HeadRevisionNum). Can be a revision number, 'LiveRevisionNum', 'LastAppliedRevisionNum', 'Tag:uuid', 'ChangeSet:uuid', etc. */
  revision?: string;
};
export type DownloadUnitDataApiResponse = /** status 200 OK */ string;
export type DownloadUnitDataApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type DestroyUnitApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ QueuedOperation;
export type DestroyUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Dry run mode - validates which units would be destroyed without executing */
  dryRun?: boolean;
};
export type GetUnitExtendedApiResponse = /** status 200 OK */ UnitExtendedRead;
export type GetUnitExtendedApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type ImportUnitApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ QueuedOperation;
export type ImportUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Dry run mode - returns import data in the operation/action */
  dryRun?: boolean;
  importRequest: ImportRequest;
};
export type DownloadUnitLiveDataApiResponse = /** status 200 OK */ string;
export type DownloadUnitLiveDataApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type DownloadUnitLiveStateApiResponse = /** status 200 OK */ string;
export type DownloadUnitLiveStateApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
};
export type ListExtendedMutationsApiResponse = /** status 200 OK */ ExtendedMutationRead[];
export type ListExtendedMutationsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Mutations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Mutation: CreatedAt, FunctionName, InvocationID, LinkID, MergeBaseRevisionNum, MergeEndRevisionNum, MergeSourceID, MutationID, MutationNum, OrganizationID, RestoredRevisionNum, RevisionID, RevisionNum, SpaceID, Subgroup, TriggerID, UnitID, UpdatedAt, UpgradedFromUpstreamRevisionNum.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Mutation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Mutation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Mutation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Mutation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Mutation are InvocationID, LinkID, MergeSourceID, OrganizationID, RevisionID, SpaceID, TriggerID, UnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Mutation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, MutationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type GetExtendedMutationApiResponse = /** status 200 OK */ ExtendedMutationRead;
export type GetExtendedMutationApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Include clause for expanding related entities in the response for Mutation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Mutation are InvocationID, LinkID, MergeSourceID, OrganizationID, RevisionID, SpaceID, TriggerID, UnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Mutation.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, MutationID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a mutation_id */
  mutationId: string;
};
export type SetUnitPredicatesApiResponse = /** status 200 OK */ UnitPredicatesResponse;
export type SetUnitPredicatesApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  unitPredicatesRequest: UnitPredicatesRequest;
};
export type RefreshUnitApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ QueuedOperation;
export type RefreshUnitApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Dry run mode - returns refresh data in the operation/action and updates LiveData and LiveState in the unit */
  dryRun?: boolean;
  /** Drift reconciliation mode. Valid values: OnDemand, ContinuousApply, ContinuousRefresh. If not specified, the current value on the Unit is used. */
  driftMode?: string;
};
export type ListExtendedRevisionsApiResponse = /** status 200 OK */ ExtendedRevisionRead[];
export type ListExtendedRevisionsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Revisions returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Revision: ApplyGates, ApplyWarnings, ApprovedBy, ChangeSetID, CreatedAt, DataHash, Description, LiveAt, OrganizationID, RevisionID, RevisionNum, Source, SpaceID, Tags, UnitID, UpdatedAt, UserAgent, UserID.
    
    To list a tagged Revision use `Tags ? '<tag-id>'`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Revision list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Revision).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Revision include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Revision are ChangeSetID, OrganizationID, SpaceID, Tags, UnitID, UserID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, RevisionID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type GetExtendedRevisionApiResponse = /** status 200 OK */ ExtendedRevisionRead;
export type GetExtendedRevisionApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Include clause for expanding related entities in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Revision are ChangeSetID, OrganizationID, SpaceID, Tags, UnitID, UserID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Revision.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, RevisionID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a revision_id */
  revisionId: string;
};
export type DownloadRevisionDataApiResponse = /** status 200 OK */ string;
export type DownloadRevisionDataApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Unique identifier for a revision_id */
  revisionId: string;
};
export type ListUnitActionsApiResponse = /** status 200 OK */ UnitAction[];
export type ListUnitActionsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of QueuedOperations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on QueuedOperation: Action, BridgeWorkerID, CreatedAt, DriftReconciliationMode, DryRun, OrganizationID, QueuedOperationID, RevisionNum, SpaceID, Status, TargetID, UnitActionNum, UnitID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the QueuedOperation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (QueuedOperation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for QueuedOperation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type GetUnitActionApiResponse =
  /** status 200 UnitAction is a record of an action to be performed by a Bridge Worker. They are queued and sent to the worker in creation order.
If the worker is temporarily disconnected the queued actions will be sent when the worker reconnects or responds.
If there are links between units applied or destroyed in a single API call, they will be sent to the appropriate
worker(s) in the appropriate order (reverse or forword topological order). One or more UnitEvents will correspond
to each UnitAction. */ UnitAction;
export type GetUnitActionApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Unique identifier for a unit_action_id */
  unitActionId: string;
};
export type ListUnitEventsApiResponse = /** status 200 OK */ UnitEventRead[];
export type ListUnitEventsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of UnitEvents returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on UnitEvent: Action, BridgeWorkerID, CreatedAt, OrganizationID, QueuedOperationID, Result, RevisionNum, SpaceID, StartedAt, Status, TerminatedAt, UnitEventID, UnitEventNum, UnitID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the UnitEvent list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (UnitEvent).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for UnitEvent include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type GetUnitEventApiResponse =
  /** status 200 UnitEvent represents an event of action performed on a Unit's configuration. Each action tracks
the lifecycle of applying, destroying, or refreshing a Unit's configuration in the target
live system. The event captures the current status of the operation, any configuration
drift detected, and timing information about when the action started and completed.
Actions are atomic from ConfigHub's perspective but may involve multiple steps
in the connected Bridge. The status and drift detection help track the health
and consistency of the provisioned configuration compared to what is defined in the Unit. */ UnitEventRead;
export type GetUnitEventApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a unit_id */
  unitId: string;
  /** Unique identifier for a unit_event_id */
  unitEventId: string;
};
export type ListViewsApiResponse = /** status 200 OK */ ExtendedViewRead[];
export type ListViewsApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Views returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on View: Annotations, CreatedAt, DisplayName, FilterID, GroupBy, Labels, Of, OrderBy, OrderByDirection, OrganizationID, Slug, SpaceID, UpdatedAt, ViewID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the View list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (View).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for View include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ViewID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type CreateViewApiResponse = /** status 200 Defines an entity view. */ ViewRead;
export type CreateViewApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  view: View;
};
export type DeleteViewApiResponse =
  /** status 200 Response for successful delete operation */ DeleteResponse;
export type DeleteViewApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a view_id */
  viewId: string;
};
export type GetViewApiResponse = /** status 200 OK */ ExtendedViewRead;
export type GetViewApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ViewID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Unique identifier for a view_id */
  viewId: string;
};
export type PatchViewApiResponse = /** status 200 Defines an entity view. */ ViewRead;
export type PatchViewApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a view_id */
  viewId: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    Columns?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    FilterID?: string | null;
    GroupBy?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Of?: string | null;
    OrderBy?: string | null;
    OrderByDirection?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type UpdateViewApiResponse = /** status 200 Defines an entity view. */ ViewRead;
export type UpdateViewApiArg = {
  /** Unique identifier for a space_id */
  spaceId: string;
  /** Unique identifier for a view_id */
  viewId: string;
  view: View;
};
export type BulkDeleteTagsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteTagsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Tags returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Tag: Annotations, ChangeSetID, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Slug, SpaceID, TagID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Tag list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Tag).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Tag include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllTagsApiResponse = /** status 200 OK */ ExtendedTagRead[];
export type ListAllTagsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Tags returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Tag: Annotations, ChangeSetID, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Slug, SpaceID, TagID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Tag list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Tag).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Tag include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TagID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchTagsApiResponse = /** status 200 OK */
  | TagCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ TagCreateOrUpdateResponseRead[];
export type BulkPatchTagsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Tags returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Tag: Annotations, ChangeSetID, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Slug, SpaceID, TagID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Tag list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Tag).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Tag include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkCreateTagsApiResponse = /** status 200 OK */
  | TagCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ TagCreateOrUpdateResponseRead[];
export type BulkCreateTagsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Tags returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Tag: Annotations, ChangeSetID, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Slug, SpaceID, TagID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Tag list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Tag).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Tag include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Tag.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Tag are ChangeSetID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Tag names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned Tag labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning tags
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkDeleteTargetsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteTargetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Targets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Target: Annotations, BridgeHandle, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, Facts, Labels, LiveStateType, Options, OrganizationID, Permissions, ProviderType, Slug, SpaceID, TargetID, ToolchainType, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Target list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Target).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Target include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Target are BridgeWorkerID, OrganizationID, SpaceID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllTargetsApiResponse = /** status 200 OK */ ExtendedTargetRead[];
export type ListAllTargetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Targets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Target: Annotations, BridgeHandle, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, Facts, Labels, LiveStateType, Options, OrganizationID, Permissions, ProviderType, Slug, SpaceID, TargetID, ToolchainType, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Target list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Target).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Target include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Target are BridgeWorkerID, OrganizationID, SpaceID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TargetID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchTargetsApiResponse = /** status 200 OK */
  | TargetCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ TargetCreateOrUpdateResponseRead[];
export type BulkPatchTargetsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Targets returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Target: Annotations, BridgeHandle, BridgeWorkerID, CreatedAt, DeleteGates, DisplayName, Facts, Labels, LiveStateType, Options, OrganizationID, Permissions, ProviderType, Slug, SpaceID, TargetID, ToolchainType, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Target list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Target).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Target include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Target.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Target are BridgeWorkerID, OrganizationID, SpaceID, TriggerFilterID, TriggerIDs.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed */
  refreshTriggers?: boolean;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    BridgeHandle?: string | null;
    BridgeWorkerID?: string | null;
    ConfigTypes?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    Facts?: {
      [key: string]: string | null;
    } | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    LiveStateType?: string | null;
    Options?: {
      [key: string]: string | null;
    } | null;
    Parameters?: string | null;
    Permissions?: {
      [key: string]: object | null;
    } | null;
    ProviderType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    TriggerFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    WhereTrigger?: string | null;
  };
};
export type BulkDeleteTriggersApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteTriggersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Trigger list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Trigger).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Trigger include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllTriggersApiResponse = /** status 200 OK */ ExtendedTriggerRead[];
export type ListAllTriggersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Trigger list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Trigger).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Trigger include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, TriggerID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchTriggersApiResponse = /** status 200 OK */
  | TriggerCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ TriggerCreateOrUpdateResponseRead[];
export type BulkPatchTriggersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Trigger list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Trigger).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Trigger include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    Disabled?: boolean | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    Event?: string | null;
    FailOpenAfter?: number | null;
    /** Function name */
    FunctionName?: string | null;
    InvocationID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    OtherDataSource?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    UnitFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Warn?: boolean | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
    WhereUnit?: string | null;
  };
};
export type BulkCreateTriggersApiResponse = /** status 200 OK */
  | TriggerCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ TriggerCreateOrUpdateResponseRead[];
export type BulkCreateTriggersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Trigger list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Trigger).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Trigger include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Trigger.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Trigger are BridgeWorkerID, InvocationID, OrganizationID, SpaceID, UnitFilterID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Trigger names */
  namePrefixes?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning triggers
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Function arguments */
    Arguments?: (object | null)[] | null;
    BridgeWorkerID?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    Description?: string | null;
    Disabled?: boolean | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    Event?: string | null;
    FailOpenAfter?: number | null;
    /** Function name */
    FunctionName?: string | null;
    InvocationID?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    OtherDataSource?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    ToolchainType?: string | null;
    UnitFilterID?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
    Warn?: boolean | null;
    /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
    WhereResource?: string | null;
    WhereUnit?: string | null;
  };
};
export type BulkDeleteUnitsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllUnitsApiResponse = /** status 200 OK */ ExtendedUnitRead[];
export type ListAllUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, UnitID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
  /** Resource type: Resource type to match for the desired ToolchainType, for example apps/v1/Deployment */
  resourceType?: string;
  /** Where data: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. The whole string must be query-encoded. */
  whereData?: string;
  /** Where expression to match Triggers. Matched triggers are invoked on each unit to filter by validation results. Use with triggers_passed to control whether passing or failing units are returned (default: failing). */
  whereTrigger?: string;
  /** Filter UUID (with From=Trigger). The filter's matching triggers are invoked on units to filter by validation results. Can be combined with where_trigger. */
  triggerFilter?: string;
  /** When true, return units that pass trigger validation; when false (default), return units that fail. Only applies when where_trigger or trigger_filter is specified. */
  triggersPassed?: boolean;
  /** View slug or UUID. Applies the View's column definitions to extract values for each unit. If the View has a FilterID, its filter is ANDed with other filters. The View must have Of=Unit or a Filter with From=Unit. */
  view?: string;
};
export type BulkPatchUnitsApiResponse = /** status 200 OK */
  | UnitCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitCreateOrUpdateResponseRead[];
export type BulkPatchUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Dry run mode: return changed unit(s) but don't update configuration data */
  dryRun?: boolean;
  /** Upgrade the unit to the latest version of its upstream unit */
  upgrade?: boolean;
  /** Restore revision source. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  restore?: string;
  /** Resolve specified non-automatically resolved link from this (downstream) Unit to another (upstream) Unit. Expects Link:uuid or Link:*. */
  resolve?: string;
  /** Merge source unit. Currently it must be a unit ID or 'Self'. */
  mergeSource?: string;
  /** Merge base revision, which provides the base configuration data of the changes to merge. With merge_source, this is a revision of the merge source unit. With merge_external_source, this is a revision of the unit being updated and overrides the default selection of the latest MergeExternal revision. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeBase?: string;
  /** Merge end revision of the merge source, which provides the final configuration of the changes to merge. Supports: Named revisions ('LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', 'HeadRevisionNum'), direct revision number (e.g., '42'), or entity references ('Tag:uuid', 'ChangeSet:uuid', 'Revision:uuid'). Can be prefixed with 'Before:' to select the revision immediately before the specified one (e.g., 'Before:LiveRevisionNum', 'Before:42'). When using Tag or ChangeSet references, the latest revision associated with that entity is selected. */
  mergeEnd?: string;
  /** Identifier of the external source for merge-on-update. When set, computes mutations between the last MergeExternal revision and the provided data, then patches the current unit data with those mutations. */
  mergeExternalSource?: string;
  /** Disable the subtraction (override-preservation) step of upgrade and merge_source. By default (false), a cross-unit merge subtracts the target's local differences from the source patch so they survive; set true to apply the source patch without subtraction, relying on stored Mutation Predicate values to preserve overrides. Has no effect on a self merge (merge_source=Self), where subtraction is always disabled. */
  mergeDisableSubtraction?: boolean;
  /** The specified string is an expression for the purpose of filtering
    the list of Mutations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Mutation: CreatedAt, FunctionName, InvocationID, LinkID, MergeBaseRevisionNum, MergeEndRevisionNum, MergeSourceID, MutationID, MutationNum, OrganizationID, RestoredRevisionNum, RevisionID, RevisionNum, SpaceID, Subgroup, TriggerID, UnitID, UpdatedAt, UpgradedFromUpstreamRevisionNum.
    
    Used to filter which mutations are affected during merge operations.
    
    The whole string must be query-encoded. */
  whereMutation?: string;
  /** UUID of a Filter entity to apply to the Mutation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Mutation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterMutation?: string;
  /** Tag ID to add to the head revision */
  tag?: string;
  /** Must match ChangeSetID of affected Units if config Data is changed unless in dry run mode */
  changeSetId?: string;
  /** User-defined category for the Mutation. Must be alphanumeric, at most 64 characters. The prefix 'ConfigHub' is reserved. */
  subgroup?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Unique identifier for the ChangeSet to which the current Revision belongs. Optional. Units are not required to belong to ChangeSets. */
    ChangeSetID?: string | null;
    /** The full configuration data for this unit. */
    Data?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** An optional set of gates that, if any is present, will block destroy operations */
    DestroyGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** LastChangeDescription is a human-readable description of the last change. This description is copied to the new Revision when the Data is changed. */
    LastChangeDescription?: string | null;
    ProviderType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** TargetID is the identifier of the target this unit is associated with. This defines where the configuration will be applied. It must be set to a valid Target within the same Space before the Unit can be Applied, Destroyed, Imported, or Refreshed. */
    TargetID?: string | null;
    /** Bridge option values set per-Unit, merged with the Target's Options when sending to the bridge worker (Target's Options take precedence on overlap). The options must be predefined by the ConfigType in the BridgeWorker. */
    TargetOptions?: {
      [key: string]: string | null;
    } | null;
    /** ToolchainType specifies the type of toolchain for this unit. Possible values include "Kubernetes/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text", "ConfigHub/YAML". */
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkCreateUnitsApiResponse = /** status 200 OK */
  | UnitCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ UnitCreateOrUpdateResponseRead[];
export type BulkCreateUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned Unit names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values for cloned Unit labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning units
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Links returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Link: Annotations, AutoUpdate, CreatedAt, DeleteGates, DisplayName, DownstreamLastMergedRevisionNum, FromUnitID, Hash, Labels, LinkID, MergeDisableSubtraction, OrganizationID, Slug, SpaceID, ToSpaceID, ToUnitID, TransformInvocationID, UpdateType, UpdatedAt, UpstreamLastMergedRevisionNum, UpstreamLinkID, UpstreamOrganizationID, UpstreamSpaceID, UseLiveState.
    
    Where expression to filter outgoing links (links to units outside the cloned set) for copying. If non-empty, matching outgoing links are also copied with FromUnitID retargeted to the cloned unit.
    
    The whole string must be query-encoded. */
  includeOutgoingLinksWhere?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    /** Unique identifier for the ChangeSet to which the current Revision belongs. Optional. Units are not required to belong to ChangeSets. */
    ChangeSetID?: string | null;
    /** The full configuration data for this unit. */
    Data?: string | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** An optional set of gates that, if any is present, will block destroy operations */
    DestroyGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    /** LastChangeDescription is a human-readable description of the last change. This description is copied to the new Revision when the Data is changed. */
    LastChangeDescription?: string | null;
    ProviderType?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** TargetID is the identifier of the target this unit is associated with. This defines where the configuration will be applied. It must be set to a valid Target within the same Space before the Unit can be Applied, Destroyed, Imported, or Refreshed. */
    TargetID?: string | null;
    /** Bridge option values set per-Unit, merged with the Target's Options when sending to the bridge worker (Target's Options take precedence on overlap). The options must be predefined by the ConfigType in the BridgeWorker. */
    TargetOptions?: {
      [key: string]: string | null;
    } | null;
    /** ToolchainType specifies the type of toolchain for this unit. Possible values include "Kubernetes/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text", "ConfigHub/YAML". */
    ToolchainType?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkApplyUnitsApiResponse = /** status 200 OK */
  | UnitActionResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitActionResponse[];
export type BulkApplyUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Dry run mode - validates which units would be applied without executing */
  dryRun?: boolean;
  /** Revision to apply (defaults to HeadRevisionNum). Can be a revision number, 'LiveRevisionNum', 'LastAppliedRevisionNum', 'Tag:uuid', 'ChangeSet:uuid', etc. */
  revision?: string;
  /** Drift reconciliation mode. Valid values: OnDemand, ContinuousApply, ContinuousRefresh. If not specified, the current value on the Unit is used. */
  driftMode?: string;
};
export type BulkApproveUnitsApiResponse = /** status 200 OK */
  | ApproveResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ ApproveResponseRead[];
export type BulkApproveUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Revision to approve (defaults to HeadRevisionNum). Can be a revision number, 'LiveRevisionNum', 'LastAppliedRevisionNum', 'Tag:uuid', 'ChangeSet:uuid', etc. */
  revision?: string;
};
export type BulkCancelUnitsApiResponse = /** status 200 OK */
  | UnitActionResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitActionResponse[];
export type BulkCancelUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type BulkDestroyUnitsApiResponse = /** status 200 OK */
  | UnitActionResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitActionResponse[];
export type BulkDestroyUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Dry run mode - validates which units would be destroyed without executing */
  dryRun?: boolean;
};
export type BulkRefreshUnitsApiResponse = /** status 200 OK */
  | UnitActionResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitActionResponse[];
export type BulkRefreshUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Dry run mode - returns refresh data in the operation/action and updates LiveData and LiveState in the unit */
  dryRun?: boolean;
  /** Drift reconciliation mode. Valid values: OnDemand, ContinuousApply, ContinuousRefresh. If not specified, the current value on the Unit is used. */
  driftMode?: string;
};
export type BulkTagUnitsApiResponse = /** status 200 OK */
  | UnitTagResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ UnitTagResponse[];
export type BulkTagUnitsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Units returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Unit: Annotations, ApplyGates, ApplyWarnings, ApprovedBy, BridgeWorkerID, ChangeSetID, CreatedAt, DataHash, DeleteGates, DestroyGates, DisplayName, DriftReconciliationMode, FromLinkID, HeadRevisionNum, HeadUnitActionNum, HeadUnitEventNum, Labels, LastActionAt, LastAppliedRevisionNum, LastChangeDescription, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, ProviderType, Slug, SpaceID, TargetID, TargetOptions, ToolchainType, UnitID, UpdatedAt, UpstreamOrganizationID, UpstreamRevisionNum, UpstreamSpaceID, UpstreamUnitID, Values.
    
    Finding all units created by cloning can be done using the expression `UpstreamRevisionNum > 0`. Clones of a specific unit can be found by additionally filtering based on `UpstreamUnitID`. Unapplied units can be found using `LiveRevisionNum = 0`. Units with unapplied changes can be found with `HeadRevisionNum > LiveRevisionNum`.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the Unit list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Unit).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for Unit include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for Unit.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for Unit are ApprovedBy, BridgeWorkerID, ChangeSetID, FromLinkID, HeadMutationNum, HeadRevisionNum, LastAppliedRevisionNum, LiveRevisionNum, OrganizationID, PreviousLiveRevisionNum, SpaceID, TargetID, UnitEventID, UpstreamSpaceID, UpstreamUnitID.
    
    The whole string must be query-encoded. */
  include?: string;
  unitTagRequest: UnitTagRequest;
};
export type ListAllUnitActionsApiResponse = /** status 200 OK */ UnitAction[];
export type ListAllUnitActionsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of QueuedOperations returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on QueuedOperation: Action, BridgeWorkerID, CreatedAt, DriftReconciliationMode, DryRun, OrganizationID, QueuedOperationID, RevisionNum, SpaceID, Status, TargetID, UnitActionNum, UnitID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the QueuedOperation list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (QueuedOperation).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for QueuedOperation include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type ListAllUnitEventsApiResponse = /** status 200 OK */ UnitEventRead[];
export type ListAllUnitEventsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of UnitEvents returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on UnitEvent: Action, BridgeWorkerID, CreatedAt, OrganizationID, QueuedOperationID, Result, RevisionNum, SpaceID, StartedAt, Status, TerminatedAt, UnitEventID, UnitEventNum, UnitID, UpdatedAt.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the UnitEvent list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (UnitEvent).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for UnitEvent include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type ListUsersApiResponse = /** status 200 OK */ UserRead[];
export type ListUsersApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Users returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on User: CreatedAt, DisplayName, ExternalID, Slug, UpdatedAt, UserID, Username.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the User list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (User).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for User include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
};
export type GetUserApiResponse = /** status 200 a User in Confighub. */ UserRead;
export type GetUserApiArg = {
  /** Unique identifier for a user_id */
  userId: string;
};
export type BulkDeleteViewsApiResponse = /** status 200 OK */
  | DeleteResponse[]
  | /** status 207 Multi-Status: Mixed success and failure results */ DeleteResponse[];
export type BulkDeleteViewsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Views returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on View: Annotations, CreatedAt, DisplayName, FilterID, GroupBy, Labels, Of, OrderBy, OrderByDirection, OrganizationID, Slug, SpaceID, UpdatedAt, ViewID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the View list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (View).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for View include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
};
export type ListAllViewsApiResponse = /** status 200 OK */ ExtendedViewRead[];
export type ListAllViewsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Views returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on View: Annotations, CreatedAt, DisplayName, FilterID, GroupBy, Labels, Of, OrderBy, OrderByDirection, OrganizationID, Slug, SpaceID, UpdatedAt, ViewID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the View list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (View).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for View include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Select clause for specifying which fields to include in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    If not specified, all fields are returned.
    Entity and parent IDs (like OrganizationID, SpaceID, ViewID) and Slug are always returned regardless of the select parameter.
    Fields used in where and contains filters are also automatically included.
    Example: 'DisplayName,CreatedAt,Labels' will return only those fields plus the required ID and Slug fields.
    The whole string must be query-encoded. */
  select?: string;
};
export type BulkPatchViewsApiResponse = /** status 200 OK */
  | ViewCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status: Mixed success and failure results */ ViewCreateOrUpdateResponseRead[];
export type BulkPatchViewsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Views returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on View: Annotations, CreatedAt, DisplayName, FilterID, GroupBy, Labels, Of, OrderBy, OrderByDirection, OrganizationID, Slug, SpaceID, UpdatedAt, ViewID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the View list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (View).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for View include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    Columns?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    FilterID?: string | null;
    GroupBy?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Of?: string | null;
    OrderBy?: string | null;
    OrderByDirection?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type BulkCreateViewsApiResponse = /** status 200 OK */
  | ViewCreateOrUpdateResponseRead[]
  | /** status 207 Multi-Status (partial success) */ ViewCreateOrUpdateResponseRead[];
export type BulkCreateViewsApiArg = {
  /** The specified string is an expression for the purpose of filtering
    the list of Views returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on View: Annotations, CreatedAt, DisplayName, FilterID, GroupBy, Labels, Of, OrderBy, OrderByDirection, OrganizationID, Slug, SpaceID, UpdatedAt, ViewID.
    
    The whole string must be query-encoded. */
  where?: string;
  /** UUID of a Filter entity to apply to the View list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (View).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filter?: string;
  /** Free text search that approximately matches the specified string against string fields and map keys/values.
    
    The search is case-insensitive and uses pattern matching to find entities containing the text.
    
    Searchable string fields include attributes like Slug, DisplayName, and string-typed custom fields.
    
    For map fields (like Labels and Annotations), the search matches both map keys and values.
    
    The search uses OR logic across all searchable fields, so matching any field will return the entity.
    
    If both 'where' and 'contains' parameters are specified, they are combined with AND logic.
    
    Searchable fields for View include string and map-type attributes from the queryable attributes list.
    
    The whole string must be query-encoded. */
  contains?: string;
  /** Include clause for expanding related entities in the response for View.
    The attribute names are case-sensitive, PascalCase, and
    expected in a comma-separated list format as in the JSON encoding.
    
    Supported attributes for View are FilterID, OrganizationID, SpaceID.
    
    The whole string must be query-encoded. */
  include?: string;
  /** Comma-separated list of prefixes to apply to cloned View names */
  namePrefixes?: string;
  /** Comma-separated list of labels with multiple values fro cloned View labels, in the format of key1=value1|value2,key2=value1|value2|value3 */
  variantLabels?: string;
  /** A string for clone names, use the prefix 'template:' for a Go-template with .SourceEntitySlug to access the original entity's slug and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}' */
  namePattern?: string;
  /** The specified string is an expression for the purpose of filtering
    the list of Spaces returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Space: Annotations, AttributeFilterID, AttributeHash, AttributeIDs, CreatedAt, DeleteGates, DisplayName, Labels, OrganizationID, Permissions, Slug, SpaceID, TriggerFilterID, TriggerHash, TriggerIDs, UpdatedAt.
    
    Where expression to select destination spaces for cloning views
    
    The whole string must be query-encoded. */
  whereSpace?: string;
  /** UUID of a Filter entity to apply to the Space list.
    
    The Filter must be in the same Organization as the user credentials.
    
    The Filter's From field must match the entity type being filtered (Space).
    
    For Space-resident entities, if the Filter has a FromSpaceID, it must match the operation's SpaceID.
    
    The Filter's Where clause will be combined with any explicit 'where' parameter using AND logic.
    
    If both 'filter' and 'where' parameters are specified, they are combined with AND logic. */
  filterSpace?: string;
  /** Allowed values are true and false. Default is false. When true, reports success when an entity already exists and returns the existing entity */
  allowExists?: string;
  body: {
    /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
    Annotations?: {
      [key: string]: string | null;
    } | null;
    Columns?: (object | null)[] | null;
    /** An optional set of gates that, if any is present, will block deletion */
    DeleteGates?: {
      [key: string]: boolean | null;
    } | null;
    /** Friendly name for the entity. */
    DisplayName?: string | null;
    FilterID?: string | null;
    GroupBy?: string | null;
    /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
    Labels?: {
      [key: string]: string | null;
    } | null;
    Of?: string | null;
    OrderBy?: string | null;
    OrderByDirection?: string | null;
    /** Unique URL-safe identifier for the entity. */
    Slug?: string | null;
    /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
    Version?: number | null;
  };
};
export type ErrorItem = {
  /** A clear explanation of what went wrong */
  Description?: string;
  /** The name of the field, resource, or entity that the error relates to */
  Item?: string;
};
export type ErrorMetadata = {
  /** Optional ID of the entity this error relates to */
  EntityID?: string;
  /** Optional slug of the entity this error relates to */
  EntitySlug?: string;
  /** Optional type of the entity this error relates to */
  EntityType?: string;
  /** Collection of error details */
  Items?: ErrorItem[];
};
export type ResponseError = {
  /** Additional context messages */
  Details?: string[];
  /** The type of error (e.g., validation, not-found) */
  ErrorCategory?: string;
  ErrorMetadata?: ErrorMetadata;
  /** The primary error message */
  Message?: string;
  /** HTTP status code */
  Status?: number;
  /** The type of error (e.g., validation, not-found) */
  Type?: string;
};
export type DeleteResponse = {
  Error?: ResponseError;
  /** Response message. */
  Message?: string;
};
export type StandardErrorResponse = {
  /** HTTP status code of the response. */
  Code?: string;
  /** Message returned with the response. */
  Message?: string;
};
export type Subjects = {
  UserIDs?: {
    [key: string]: boolean;
  };
};
export type Permissions = {
  [key: string]: Subjects;
};
export type Space = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Reference to a Filter entity used to identify Attributes for the Space's FunctionExecutor. The Filter's From field must be set to 'Attribute'. */
  AttributeFilterID?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  Permissions?: Permissions;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Reference to a Filter entity used to identify Triggers that should be invoked on Units within this Space. The Filter's From field must be set to 'Trigger'. */
  TriggerFilterID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Filter expression to identify Attributes that should be registered in the Space's FunctionExecutor. The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  WhereAttribute?: string;
  /** Filter expression to identify Triggers that should be invoked on Units within this Space. The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  WhereTrigger?: string;
};
export type Uuid = string;
export type SpaceRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Reference to a Filter entity used to identify Attributes for the Space's FunctionExecutor. The Filter's From field must be set to 'Attribute'. */
  AttributeFilterID?: string;
  /** Hash of all registered Attribute configurations for the Space. (readonly) */
  AttributeHash?: string;
  /** List of Attribute IDs that match the WhereAttribute and/or AttributeFilterID criteria. (readonly) */
  AttributeIDs?: Uuid[];
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  Permissions?: Permissions;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Reference to a Filter entity used to identify Triggers that should be invoked on Units within this Space. The Filter's From field must be set to 'Trigger'. */
  TriggerFilterID?: string;
  TriggerHash?: string;
  /** List of Trigger IDs that match the WhereTrigger and/or TriggerFilterID criteria. (readonly) */
  TriggerIDs?: Uuid[];
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Filter expression to identify Attributes that should be registered in the Space's FunctionExecutor. The specified string is an expression for the purpose of filtering
    the list of Attributes returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Attribute: Annotations, AttributeID, CreatedAt, DataType, DeleteGates, DisplayName, Hash, Labels, OrganizationID, Slug, SpaceID, ToolchainType, UpdatedAt.
    
    The whole string must be query-encoded. */
  WhereAttribute?: string;
  /** Filter expression to identify Triggers that should be invoked on Units within this Space. The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  WhereTrigger?: string;
};
export type SpaceCreateOrUpdateResponse = {
  Error?: ResponseError;
  Space?: Space;
};
export type SpaceCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Space?: SpaceRead;
};
export type Schema = any;
export type FunctionParameter = {
  /** Data type of the parameter */
  DataType?: string;
  /** Description of the parameter */
  Description?: string;
  /** List of valid enum values; applies to enum parameters */
  EnumValues?: string[];
  /** Example value */
  Example?: string;
  /** Maximum allowed value; applies to int parameters */
  Max?: number | null;
  /** Minimum allowed value; applies to int parameters */
  Min?: number | null;
  /** Name of the parameter in kabob-case */
  ParameterName?: string;
  /** Regular expression matching valid values; applies to string parameters */
  Regexp?: string;
  /** Whether the parameter is required */
  Required?: boolean;
  Schema?: Schema;
};
export type FunctionArgument = {
  Evaluator?: string;
  ParameterName?: string;
  Value?: string | number | boolean;
};
export type FunctionInvocation = {
  /** Function arguments */
  Arguments?: FunctionArgument[] | null;
  /** Function name */
  FunctionName?: string;
  /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
  WhereResource?: string;
};
export type AttributeDetails = {
  /** ID of the Link that bound this needed path to a provided value */
  BoundLinkID?: string;
  /** ProvidedProperties of the provided path that was bound to this needed path, for cross-link score comparison */
  BoundProvidedProperties?: {
    [key: string]: string;
  };
  /** Description of the attribute */
  Description?: string;
  GetterInvocation?: FunctionInvocation;
  /** Whether this attribute is a needed value */
  IsNeeded?: boolean;
  /** Whether this attribute is a provided value */
  IsProvided?: boolean;
  /** Preferred properties for matching; more matches produce a stronger match preference */
  NeededPreferred?: {
    [key: string]: string;
  };
  /** Required properties that a provided value must have in order to match */
  NeededRequired?: {
    [key: string]: string;
  };
  /** Key/value properties describing what this provided value offers, for matching */
  ProvidedProperties?: {
    [key: string]: string;
  };
  /** Function invocation used to set the attribute (except for the value), if any */
  SetterInvocations?: FunctionInvocation[];
};
export type PathVisitorInfo = {
  /** AttributeName for the path */
  AttributeName?: string;
  /** DataType of the attribute at the path */
  DataType?: string;
  Details?: AttributeDetails;
  /** Configuration of the embedded accessor, if any */
  EmbeddedAccessorConfig?: string;
  /** Embedded accessor to use, if any */
  EmbeddedAccessorType?: string;
  /** Unresolved path pattern */
  Path?: string;
  /** Specific resolved path */
  ResolvedPath?: string;
  /** Resource types to skip */
  TypeExceptions?: {
    [key: string]: object;
  };
};
export type PathToVisitorInfoType = {
  [key: string]: PathVisitorInfo;
};
export type ResourceTypePathsEntry = {
  /** ID of the Link that bound this needed path to a provided value */
  BoundLinkID?: string;
  /** ProvidedProperties of the provided path that was bound to this needed path, for cross-link score comparison */
  BoundProvidedProperties?: {
    [key: string]: string;
  };
  GetterInvocation?: FunctionInvocation;
  /** Whether this attribute is a needed value */
  IsNeeded?: boolean;
  /** Whether this attribute is a provided value */
  IsProvided?: boolean;
  /** Preferred properties for matching; more matches produce a stronger match preference */
  NeededPreferred?: {
    [key: string]: string;
  };
  /** Required properties that a provided value must have in order to match */
  NeededRequired?: {
    [key: string]: string;
  };
  Paths?: PathToVisitorInfoType;
  /** Key/value properties describing what this provided value offers, for matching */
  ProvidedProperties?: {
    [key: string]: string;
  };
  ResourceType?: string;
  SetterInvocation?: FunctionInvocation;
};
export type Attribute = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** AttributeID uniquely identifies an attribute within the system. */
  AttributeID?: string;
  /** DataType specifies the data type of the attribute value. Must be one of: string, int, bool. */
  DataType: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Description describes the attribute and its generated functions. */
  Description?: string;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Parameters specifies the function parameters for the getter and setter functions. */
  Parameters?: FunctionParameter[] | null;
  /** ResourceTypePaths maps resource types to their path-to-visitor-info mappings. */
  ResourceTypePaths?: ResourceTypePathsEntry[] | null;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** ToolchainType specifies the type of toolchain this attribute works with. */
  ToolchainType: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type AttributeRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** AttributeID uniquely identifies an attribute within the system. */
  AttributeID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** DataType specifies the data type of the attribute value. Must be one of: string, int, bool. */
  DataType: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Description describes the attribute and its generated functions. */
  Description?: string;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Hash is a SHA256 hash of the attribute's defining properties. (readonly) */
  Hash?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Parameters specifies the function parameters for the getter and setter functions. */
  Parameters?: FunctionParameter[] | null;
  /** ResourceTypePaths maps resource types to their path-to-visitor-info mappings. */
  ResourceTypePaths?: ResourceTypePathsEntry[] | null;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** ToolchainType specifies the type of toolchain this attribute works with. */
  ToolchainType: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type Organization = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Unique email domain name for the External Identity Provider record matching this organization */
  EmailDomain?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type OrganizationRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Unique email domain name for the External Identity Provider record matching this organization */
  EmailDomain?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Unique identifier for the External Identity Provider record matching this organization. */
  ExternalID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ExtendedAttribute = {
  Attribute?: Attribute;
  Error?: ResponseError;
  Organization?: Organization;
  Space?: Space;
};
export type ExtendedAttributeRead = {
  Attribute?: AttributeRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
};
export type AttributeCreateOrUpdateResponse = {
  Attribute?: Attribute;
  Error?: ResponseError;
};
export type AttributeCreateOrUpdateResponseRead = {
  Attribute?: AttributeRead;
  Error?: ResponseError;
};
export type TargetType2 = {
  /** Identifier used by the Bridge to refer to discovered/enabled Target credentials and coordinates */
  BridgeHandle?: string;
  /** Used to set the Slug and DisplayName of the Target created in ConfigHub. Optional. */
  Name?: string;
  /** Deprecated. Used to set the Parameters of the Target created in ConfigHub */
  Params?: {
    [key: string]: any;
  };
};
export type BridgeOption = {
  /** Data type of the option */
  DataType?: string;
  /** Description of the option */
  Description?: string;
  /** Example value */
  Example?: string;
  /** Name of the option in PascalCase */
  Name?: string;
  /** Whether the option is required */
  Required?: boolean;
};
export type SupportedConfigType = {
  /** Targets known by the BridgeWorker. Optional. */
  AvailableTargets?: TargetType2[];
  /** Bridge with compatible BridgeHandles */
  CompatibleBridge?: string;
  /** Configuration toolchain and format of the LiveState for this bridge; required in order to invoke functions on LiveState */
  LiveStateType?: string;
  /** Supported bridge options */
  Options?: BridgeOption[];
  /** Type identifying a bridge implementation supported by the worker */
  ProviderType?: string;
  /** Configuration toolchain and format implemented by this bridge of the worker */
  ToolchainType?: string;
};
export type BridgeWorkerInfo = {
  /** Configuration types of the bridges supported by the worker */
  SupportedConfigTypes?: SupportedConfigType[];
};
export type FunctionOutput = {
  /** Description of the result */
  Description?: string;
  /** Data type of the JSON embedded in the output */
  OutputType?: string;
  /** Name of the result in kabob-case */
  ResultName?: string;
  Schema?: Schema;
};
export type FunctionSignature = {
  /** Resource types the function applies to; * if all */
  AffectedResourceTypes?: string[];
  /** Attribute corresponding to registered paths, if a path visitor; optional */
  AttributeName?: string;
  /** Description of the function */
  Description?: string;
  /** Name of the function in kabob-case */
  FunctionName?: string;
  /** Implementation pattern of the function: PathVisitor or Custom */
  FunctionType?: string;
  /** Does not call other systems */
  Hermetic?: boolean;
  /** Will return the same result if invoked again */
  Idempotent?: boolean;
  /** May change the configuration data */
  Mutating?: boolean;
  /** If non-empty, specification of what source(s) are expected in OtherData; if empty, OtherData is not used */
  OtherDataExpected?: string[];
  OutputInfo?: FunctionOutput;
  /** Function parameters, in order */
  Parameters?: FunctionParameter[] | null;
  /** Number of required parameters */
  RequiredParameters?: number;
  /** Toolchain under which the function is registered */
  ToolchainType?: string;
  /** Returns ValidationResult */
  Validating?: boolean;
  /** Last parameter may be repeated */
  VarArgs?: boolean;
};
export type FunctionWorkerInfo = {
  /** Signatures of supported functions by ToolchainType */
  SupportedFunctions?: {
    [key: string]: {
      [key: string]: FunctionSignature;
    };
  } | null;
  /** Supported ToolchainTypes */
  ToolchainTypes?: string[] | null;
};
export type WorkerInfo = {
  BridgeWorkerInfo?: BridgeWorkerInfo;
  FunctionWorkerInfo?: FunctionWorkerInfo;
  /** If true, this is a server-hosted worker. */
  IsServerWorker?: boolean;
  /** If true, the server worker operates using the requesting user's identity rather than the worker's bot identity. Requires IsServerWorker to be true. */
  UseUserIdentity?: boolean;
};
export type BridgeWorker = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Unique identifier for a Bridge Worker. */
  BridgeWorkerID?: string;
  /** Condition represents the worker's readiness state (Ready, NotReady, Unresponsive, Disconnected). */
  Condition?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Organization-level permission for the BridgeWorker User. */
  OrgRole?: string;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  Permissions?: Permissions;
  ProvidedInfo?: WorkerInfo;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type BridgeWorkerRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Unique identifier for a Bridge Worker. */
  BridgeWorkerID?: string;
  ClaimTTLSeconds?: number;
  ClaimToken?: string;
  /** Condition represents the worker's readiness state (Ready, NotReady, Unresponsive, Disconnected). */
  Condition?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** IPAddress is the IP address from which the worker last connected. */
  IPAddress?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** LastMessage contains the last message from the worker (heartbeat message or other status). */
  LastMessage?: string;
  /** LastSeenAt is the time the worker was last seen (heartbeat, connection, or any event). */
  LastSeenAt?: string;
  /** Organization-level permission for the BridgeWorker User. */
  OrgRole?: string;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  Permissions?: Permissions;
  ProvidedInfo?: WorkerInfo;
  /** Secret is a unique secret token for the bridge worker.
    It's auto-generated when the BridgeWorker entity is created and cannot be modified.
    This field is output-only and used for authentication.
    This secret is required when starting the bridge worker program. */
  Secret?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** UserID is the ID of the bot user associated with this bridge worker.
    This user is created when the bridge worker is created and is used for
    audit trails and permissions.
    For legacy workers, this field may be nil (zero UUID). */
  UserID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ExtendedBridgeWorker = {
  BridgeWorker?: BridgeWorker;
  Error?: ResponseError;
  Organization?: Organization;
  Space?: Space;
  TargetCount?: number;
};
export type ExtendedBridgeWorkerRead = {
  BridgeWorker?: BridgeWorkerRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  TargetCount?: number;
};
export type BridgeWorkerCreateOrUpdateResponse = {
  BridgeWorker?: BridgeWorker;
  Error?: ResponseError;
};
export type BridgeWorkerCreateOrUpdateResponseRead = {
  BridgeWorker?: BridgeWorkerRead;
  Error?: ResponseError;
};
export type ActionType =
  | 'Apply'
  | 'Destroy'
  | 'Finalize'
  | 'Heartbeat'
  | 'Import'
  | 'N/A'
  | 'Refresh';
export type ResourceStatus = {
  /** Human-readable status details or error message */
  Message?: string;
  /** Health state from kstatus (Ready, InProgress, Failed, Unknown) */
  Readiness?: string;
  /** Whether config was pushed to the target (Synced or NotSynced) */
  SyncStatus?: string;
  /** Timestamp when this resource status was last updated */
  UpdatedAt?: string;
};
export type ResourceStatusMap = {
  [key: string]: ResourceStatus;
};
export type ActionResultType =
  | 'ApplyFailed'
  | 'ApplyWaitFailed'
  | 'ApplyCompleted'
  | 'DestroyCompleted'
  | 'DestroyWaitFailed'
  | 'DestroyFailed'
  | 'ImportCompleted'
  | 'ImportFailed'
  | 'RefreshAndDrifted'
  | 'RefreshAndNoDrift'
  | 'RefreshFailed'
  | 'None';
export type ActionStatusType =
  | 'None'
  | 'Pending'
  | 'Submitted'
  | 'Progressing'
  | 'Completed'
  | 'Failed'
  | 'Canceled'
  | 'Aborted';
export type ActionResult = {
  Action?: ActionType;
  /** Additional state used by the Bridge */
  BridgeState?: string;
  /** Updated configuration Data of the Unit (for refresh and import) */
  Data?: string;
  /** Warning or error messages to surface to the user */
  ErrorMessages?: string[];
  /** Live Data corresponding to the Unit (for inventory and drift detection) */
  LiveData?: string;
  /** Live State corresponding to the Unit (for status determination) */
  LiveState?: string;
  Message?: string;
  /** UUID of the operation corresponding to the action request */
  QueuedOperationID?: string;
  ResourceStatuses?: ResourceStatusMap;
  Result?: ActionResultType;
  RevisionNum?: number;
  /** UUID of the Space of the Unit on which the action is performed */
  SpaceID?: string;
  StartedAt?: string;
  Status?: ActionStatusType;
  TerminatedAt?: string | null;
  /** UUID of the Unit on which the action is performed */
  UnitID?: string;
};
export type QueuedOperation = {
  Action?: ActionType;
  BridgeState?: string;
  /** BridgeWorkerID is the unique identifier of the bridge worker that will process this operation. */
  BridgeWorkerID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** The result of a dry-run Data-changing action like refresh and import, where the data is not stored in the Unit. */
  Data?: string;
  /** Dependencies contains the list of operation IDs that this operation depends on. Operations will not be delivered until all dependencies are completed. */
  Dependencies?: Uuid[] | null;
  /** The drift reconciliation mode for the unit at the time of the operation. */
  DriftReconciliationMode?: string;
  /** DryRun indicates whether the action is a dry run. */
  DryRun?: boolean;
  /** Error details returned by the worker. */
  ErrorDetails?: ErrorItem[];
  /** ExtraParams contains additional parameters for the operation in string format. */
  ExtraParams?: string;
  LiveData?: string;
  LiveState?: string;
  /** OrganizationID is the unique identifier of the organization this operation belongs to. */
  OrganizationID?: string;
  /** QueuedOperationID is the unique identifier for the queued unit action. */
  QueuedOperationID?: string;
  /** RevisionNum is the revision number this operation was performed on. */
  RevisionNum?: number;
  /** SpaceID is the unique identifier of the space of the unit this operation is performed on. */
  SpaceID?: string;
  /** Status indicates the current status of the unit action. v2 statuses: Initializing (being set up), Pending (waiting), Delivered (sent to worker), Progressing (being processed), Completed (success), Failed (error). v1 compatibility: 'pending' = Pending, 'delivered' = Completed (legacy 'delivered' meant work done). */
  Status?:
    | 'Initializing'
    | 'Pending'
    | 'Delivered'
    | 'Progressing'
    | 'Completed'
    | 'Failed'
    | 'Aborted'
    | 'Canceled'
    | 'pending'
    | 'delivered';
  /** TargetID is the unique identifier of the target this operation is directed to. */
  TargetID?: string;
  /** UnitActionNum is the sequence number of this unit action. */
  UnitActionNum?: number;
  /** UnitID is the unique identifier of the unit this operation is performed on. */
  UnitID?: string;
  /** User-Agent string of the API call. */
  UserAgent?: string;
  /** UserID of the user the action was performed by. */
  UserID?: string;
  /** Whether the user who requested the action is a bot user. */
  UserIsBot?: boolean;
  /** Organization-level role of the user who requested the action. */
  UserRole?: string;
  /** An entity-specific sequence number used for optimistic concurrency control.
    The value read must be sent in calls to Update. */
  Version?: number;
};
export type EventMessage = {
  Data?: string;
  Event?: string;
};
export type ChangeSet = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** ChangeSetID uniquely identifies a changeset within the system. */
  ChangeSetID?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Description is a human-readable description of the change. */
  Description?: string;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ChangeSetRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** ChangeSetID uniquely identifies a changeset within the system. */
  ChangeSetID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Description is a human-readable description of the change. */
  Description?: string;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** EndTagID is the identifier of the set of revisions that end the ChangeSet. */
  EndTagID?: string;
  /** The type of entity. */
  EntityType?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** StartTagID is the identifier of the set of revisions that begin the ChangeSet. */
  StartTagID?: string;
  /** State represents the current state of the ChangeSet. */
  State?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type Tag = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** TagID uniquely identifies a tag within the system. */
  TagID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type TagRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** ChangeSetID is the optional ID of the ChangeSet this Tag is associated with. */
  ChangeSetID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** TagID uniquely identifies a tag within the system. */
  TagID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ExtendedChangeSet = {
  ChangeSet?: ChangeSet;
  EndTag?: Tag;
  Error?: ResponseError;
  Organization?: Organization;
  Space?: Space;
  StartTag?: Tag;
};
export type ExtendedChangeSetRead = {
  ChangeSet?: ChangeSetRead;
  EndTag?: TagRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  StartTag?: TagRead;
};
export type ChangeSetCreateOrUpdateResponse = {
  ChangeSet?: ChangeSet;
  Error?: ResponseError;
};
export type ChangeSetCreateOrUpdateResponseRead = {
  ChangeSet?: ChangeSetRead;
  Error?: ResponseError;
};
export type Filter = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** FilterID uniquely identifies a filter within the system. */
  FilterID?: string;
  /** From specifies the type of entity (Unit, Space, etc.) to filter, in PascalCase. */
  From: string;
  /** FromSpaceID optionally specifies a Space to filter within. Only relevant for spaced entity spaces. (optional) */
  FromSpaceID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Resource type to match for the desired ToolchainType, for example apps/v1/Deployment. Valid only for Units. (optional) */
  ResourceType?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Where specifices the where filter expression in the syntax used in list and search API query parameters. (optional) */
  Where?: string;
  /** WhereData specifies a filter expression for configuration data. Valid only for Units. (optional) */
  WhereData?: string;
};
export type FilterRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** FilterID uniquely identifies a filter within the system. */
  FilterID?: string;
  /** From specifies the type of entity (Unit, Space, etc.) to filter, in PascalCase. */
  From: string;
  /** FromSpaceID optionally specifies a Space to filter within. Only relevant for spaced entity spaces. (optional) */
  FromSpaceID?: string;
  /** SHA256 hash of the filter parameters encoded as hexadecimal. (readonly) */
  Hash?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Resource type to match for the desired ToolchainType, for example apps/v1/Deployment. Valid only for Units. (optional) */
  ResourceType?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Where specifices the where filter expression in the syntax used in list and search API query parameters. (optional) */
  Where?: string;
  /** WhereData specifies a filter expression for configuration data. Valid only for Units. (optional) */
  WhereData?: string;
};
export type ExtendedFilter = {
  Error?: ResponseError;
  Filter?: Filter;
  FromSpace?: Space;
  Organization?: Organization;
  Space?: Space;
};
export type ExtendedFilterRead = {
  Error?: ResponseError;
  Filter?: FilterRead;
  FromSpace?: SpaceRead;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
};
export type FilterCreateOrUpdateResponse = {
  Error?: ResponseError;
  Filter?: Filter;
};
export type FilterCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Filter?: FilterRead;
};
export type ArrayElementAliasMap = {
  [key: string]: {
    [key: string]: string;
  };
};
export type ArrayOrderMap = {
  [key: string]: string[];
};
export type MutationType = 'Add' | 'Delete' | 'Update' | 'Replace' | 'None';
export type MutationInfo = {
  /** Function index or sequence number corresponding to the change */
  Index?: number;
  MutationType?: MutationType;
  /** Line-level patch for multi-line string updates, in unified diff format. When present on an Update, PatchMutations applies this to the target value instead of replacing with Value. Falls back to Value if the patch cannot be applied cleanly. */
  Patch?: string;
  /** Used to decide how to use the mututation */
  Predicate?: boolean;
  /** Removed configuration data if MutationType is Delete and otherwise the new data */
  Value?: string;
};
export type MutationMap = {
  [key: string]: MutationInfo;
};
export type ResourceInfo = {
  /** Category of configuration element represented in the configuration data; Kubernetes resources are of category Resource, and application configuration files are of category AppConfig */
  ResourceCategory?: string;
  /** Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID */
  ResourceName?: string;
  /** Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip */
  ResourceNameStableCore?: string;
  /** Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name> */
  ResourceNameWithoutScope?: string;
  /** Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind) */
  ResourceType?: string;
};
export type ResourceMutation = {
  /** Names (with scopes, if any) used in current and prior revisions of this resource */
  Aliases?: {
    [key: string]: object;
  };
  /** Names without scopes used in current and prior revisions of this resource */
  AliasesWithoutScopes?: {
    [key: string]: object;
  };
  ArrayElementAliases?: ArrayElementAliasMap;
  ArrayOrders?: ArrayOrderMap;
  PathMutationMap?: MutationMap;
  Resource?: ResourceInfo;
  ResourceMutationInfo?: MutationInfo;
};
export type ResourceMutationList = ResourceMutation[] | null;
export type FunctionInvocationsResponse = {
  /** The resulting configuration data, potentially mutated */
  ConfigData?: string;
  Error?: ResponseError;
  /** Functions produced new mutations (of type other than None) */
  HasNewMutations?: boolean;
  Mutations?: ResourceMutationList;
  /** List of function invocation indices that resulted in mutations */
  Mutators?: number[] | null;
  /** ID of the Unit's Organization */
  OrganizationID?: string;
  /** Map of output types to their corresponding output data as embedded JSON */
  Outputs?: {
    [key: string]: string;
  } | null;
  /** ID of the Revision the configuration data is associated with */
  RevisionID?: string;
  /** ID of the Unit's Space */
  SpaceID?: string;
  /** Slug of the Unit's Space */
  SpaceSlug?: string;
  /** True if all functions executed successfully */
  Success?: boolean;
  /** ID of the Unit the configuration data is associated with */
  UnitID?: string;
  /** Slug of the Unit */
  UnitSlug?: string;
};
export type FunctionInvocationList = FunctionInvocation[] | null;
export type FunctionInvocationsRequest = {
  /** BridgeWorkerID is the identifier of the associated worker that will execute these functions. */
  BridgeWorkerID?: string;
  /** ChangeDescription is a description of the change being made, if any. */
  ChangeDescription?: string;
  FunctionInvocations?: FunctionInvocationList;
  /** Invocations is a list of Invocation IDs to execute. The invocations must be within the same Organization. Invocations will be executed after the FunctionInvocations list. Functions are grouped by executor (built-in vs bridge worker) and executed in phases: general mutating functions first, then final mutating functions (like ensure-context), then validating functions. Functions that don't match the unit's toolchain type are ignored. */
  Invocations?: Uuid[];
  /** NumFilters is the number of validating functions from the FunctionInvocations to treat as filters for the remaining functions in the list. In the case that the validation function does not pass, stop and don't execute the remaining functions, but don't report an error. */
  NumFilters?: number;
  /** OnLiveState indicates that the functions should be invoked on the LiveState rather than the Data. */
  OnLiveState?: boolean;
  /** StopOnError indicates whether to stop executing functions from the FunctionInvocations list on the first error, or to execute all of the functions and return all of the errors. Note that this applies to each Unit or Revision individually rather than all of the entities on which the functions are being invoked. */
  StopOnError?: boolean;
  /** ToolchainType specifies the type of toolchain for these function invocations. This determines which configuration formats the functions can process. If OnLiveState is false, it must match the ToolchainType of the Units. If OnLiveState is true, it must match the LiveStateType of the Targets of the Units. */
  ToolchainType?: string;
  /** Triggers is a list of Trigger IDs to execute. The triggers must be within the same Organization. Triggers will be executed after the FunctionInvocations list. Functions are grouped by executor (built-in vs bridge worker) and executed in phases: general mutating functions first, then final mutating functions (like ensure-context), then validating functions. Functions that don't match the unit's toolchain type are ignored. */
  Triggers?: Uuid[];
  UpdateApplyGates?: boolean;
  /** WhereResource restricts which resources functions operate on using ConfigHub metadata path expressions (ConfigHub.ResourceName, ConfigHub.ResourceNameWithoutScope, ConfigHub.ResourceType, ConfigHub.ResourceCategory). */
  WhereResource?: string;
};
export type ApiInfo = {};
export type ApiInfoRead = {
  AuthServer?: string;
  /** Build identifier for support cases. */
  Build?: string;
  /** The timestamp when ConfigHub was built in "2023-01-01T12:00:00Z" format for support cases. */
  BuiltAt?: string;
  /** ClientID for identity provider service. */
  ClientID?: string;
  DeviceAuthURL?: string;
  DeviceTokenURL?: string;
  /** OCI registry host for pulling configuration artifacts. */
  OCIHost?: string;
  /** OCI registry port for pulling configuration artifacts. */
  OCIPort?: string;
  RedirectURI?: string;
  /** Semantic version of the server (e.g. v1.2.3), or 'dev' for development builds. */
  Version?: string;
  /** Port number for the worker to connect to the server. */
  WorkerPort?: string;
};
export type Invocation = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Function arguments */
  Arguments?: FunctionArgument[] | null;
  /** Unique identifier for a Bridge Worker to execute the function specified by the Invocation. If unspecified, use the builtin function executor. */
  BridgeWorkerID?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Function name */
  FunctionName?: string;
  /** InvocationID uniquely identifies a invocation within the system. */
  InvocationID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** ToolchainType specifies the type of toolchain this invocation works with.
            This determines which configuration formats the invocation can process. */
  ToolchainType: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
  WhereResource?: string;
};
export type InvocationRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Function arguments */
  Arguments?: FunctionArgument[] | null;
  /** Unique identifier for a Bridge Worker to execute the function specified by the Invocation. If unspecified, use the builtin function executor. */
  BridgeWorkerID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Function name */
  FunctionName?: string;
  /** SHA256 hash of the function name and arguments encoded as hexadecimal. */
  Hash?: string;
  /** InvocationID uniquely identifies a invocation within the system. */
  InvocationID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** ToolchainType specifies the type of toolchain this invocation works with.
            This determines which configuration formats the invocation can process. */
  ToolchainType: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource). */
  WhereResource?: string;
};
export type ExtendedInvocation = {
  BridgeWorker?: BridgeWorker;
  Error?: ResponseError;
  Invocation?: Invocation;
  Organization?: Organization;
  Space?: Space;
};
export type ExtendedInvocationRead = {
  BridgeWorker?: BridgeWorkerRead;
  Error?: ResponseError;
  Invocation?: InvocationRead;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
};
export type InvocationCreateOrUpdateResponse = {
  Error?: ResponseError;
  Invocation?: Invocation;
};
export type InvocationCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Invocation?: InvocationRead;
};
export type Unit = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Unique identifier for the ChangeSet to which the current Revision belongs. Optional. Units are not required to belong to ChangeSets. */
  ChangeSetID?: string;
  /** The full configuration data for this unit. The maximum size is 67108864 bytes. */
  Data?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** An optional set of gates that, if any is present, will block destroy operations. */
  DestroyGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** LastChangeDescription is a human-readable description of the last change. This description is copied to the new Revision when the Data is changed. */
  LastChangeDescription?: string;
  MutationSources?: ResourceMutationList;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** ProviderType identifies which bridge to use in the case that the Target supports multiple ProviderTypes. */
  ProviderType?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** TargetID is the identifier of the target this unit is associated with. This defines where the configuration will be applied. It must be set to a valid Target within the same Space before the Unit can be Applied, Destroyed, Imported, or Refreshed. */
  TargetID?: string;
  /** Bridge option values set per-Unit, merged with the Target's Options when sending to the bridge worker (Target's Options take precedence on overlap). The options must be predefined by the ConfigType in the BridgeWorker. */
  TargetOptions?: {
    [key: string]: string;
  };
  /** ToolchainType specifies the type of toolchain for this unit. Possible values include "Kubernetes/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text", "ConfigHub/YAML". */
  ToolchainType: string;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type Issue = {
  /** Identifier for the kind of issue found, such as a number, alphanumeric code, or policy/rule name. Use especially with validation functions that check multiple policies/rules. */
  Identifier?: string;
  /** A more detailed and specific explanation of the issue found */
  Message?: string;
};
export type AttributeValue = {
  /** Name of the registered attribute */
  AttributeName?: string;
  /** Line comment on the attribute at the specified Path */
  Comment?: string;
  /** Data type if the attribute value. */
  DataType?: string;
  Details?: AttributeDetails;
  /** Name of the function invocation corresponding to the output */
  FunctionName?: string;
  /** True if a path in the live state, false if a path in the configuration data */
  InLiveState?: boolean;
  /** Index of the function invocation corresponding to the output. Useful in the case that multiple function invocations in the same executor call return AttributeValueList output. */
  Index?: number;
  /** Issues found with the attribute */
  Issues?: Issue[];
  /** Path of the attribute */
  Path?: string;
  /** Category of configuration element represented in the configuration data; Kubernetes resources are of category Resource, and application configuration files are of category AppConfig */
  ResourceCategory?: string;
  /** Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID */
  ResourceName?: string;
  /** Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip */
  ResourceNameStableCore?: string;
  /** Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name> */
  ResourceNameWithoutScope?: string;
  /** Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind) */
  ResourceType?: string;
  /** Score of finding attributed to this Path */
  Score?: string;
  /** Value of the attribute at the specified Path */
  Value?: any;
};
export type AttributeInfo = {
  /** Name of the registered attribute */
  AttributeName?: string;
  /** Data type if the attribute value. */
  DataType?: string;
  Details?: AttributeDetails;
  /** True if a path in the live state, false if a path in the configuration data */
  InLiveState?: boolean;
  /** Path of the attribute */
  Path?: string;
  /** Category of configuration element represented in the configuration data; Kubernetes resources are of category Resource, and application configuration files are of category AppConfig */
  ResourceCategory?: string;
  /** Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID */
  ResourceName?: string;
  /** Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip */
  ResourceNameStableCore?: string;
  /** Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name> */
  ResourceNameWithoutScope?: string;
  /** Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind) */
  ResourceType?: string;
};
export type AttributeValueList = AttributeValue[];
export type ValidationResult = {
  /** Deprecated. Use Issues or FailedAttributes instead. Optional list of failure details when not associated with specific attributes/paths. */
  Details?: string[];
  FailedAttributes?: AttributeValueList;
  /** Name of the function invocation corresponding to the result */
  FunctionName?: string;
  /** Index of the function invocation corresponding to the result. Useful in the case that multiple function invocations in the same executor call return ValidationResultList output. */
  Index?: number;
  /** Issues found with the configuration unit that are not associated with specific attributes/paths. Use FailedAttributes where possible. */
  Issues?: Issue[];
  /** Maximum score of all findings */
  MaxScore?: string;
  /** True if valid, false otherwise */
  Passed?: boolean;
};
export type ValidationResultList = ValidationResult[];
export type UnitRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers invoking validating functions that did not pass on the latest configuration data. These block Apply operations. */
  ApplyGates?: {
    [key: string]: boolean;
  };
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers with Warn=true invoking validating functions that did not pass on the latest configuration data. These do not block Apply operations. */
  ApplyWarnings?: {
    [key: string]: boolean;
  };
  /** The users that have approved the latest revision of the config data for the Unit. */
  ApprovedBy?: Uuid[] | null;
  /** Additional state used by the Bridge; content is ProviderType-specific. */
  BridgeState?: string;
  /** ID of the BridgeWorker from the Target assigned to this Unit. */
  BridgeWorkerID?: string;
  /** Unique identifier for the ChangeSet to which the current Revision belongs. Optional. Units are not required to belong to ChangeSets. */
  ChangeSetID?: string;
  /** Deprecated: Use DataHash instead. The CRC32 hash of the configuration data. */
  ContentHash?: number;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** The full configuration data for this unit. The maximum size is 67108864 bytes. */
  Data?: string;
  /** The SHA256 hash of the configuration data, encoded as hexadecimal. */
  DataHash?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** An optional set of gates that, if any is present, will block destroy operations. */
  DestroyGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** When the drift reconciliation mode is OnDemand, then the live state of the Target is updated only on Apply actions and the unit Data is updated only on Refresh actions. When the mode is ContinuousApply the live state is updated to match the last applied state when it has drifted from that state. When the mode is ContinuousRefresh, the unit Data is updated when it has drifted from the live state. The mode can be changed via the drift_mode parameter on Apply and Refresh operations. If the drift reconciliation mode is set in the opposing direction on the Unit (i.e., ContinuousApply when Refresh is invoked or ContinuousRefresh when Apply is invoked) and is not changed to a compatible value, then the operation will fail. */
  DriftReconciliationMode?: string;
  /** The type of entity. */
  EntityType?: string;
  /** IDs of Links originating from this Unit. */
  FromLinkID?: Uuid[] | null;
  /** Sequence number the head Mutation. */
  HeadMutationNum?: number;
  /** Sequence number the head Revision. */
  HeadRevisionNum?: number;
  /** Sequence number of the head unit action (queued operation). */
  HeadUnitActionNum?: number;
  /** Sequence number of the head unit event. */
  HeadUnitEventNum?: number;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Sequence number the last Revision applied. 0 if no live revision. */
  LastAppliedRevisionNum?: number;
  /** LastChangeDescription is a human-readable description of the last change. This description is copied to the new Revision when the Data is changed. */
  LastChangeDescription?: string;
  /** The live resources as of the most recent non-dry-run action in the same representation as Data. */
  LiveData?: string;
  /** Sequence number the last Revision applied once apply has completed. 0 if no live revision. */
  LiveRevisionNum?: number;
  /** The live state as of the most recent non-dry-run action; content is ProviderType-specific. */
  LiveState?: string;
  MutationSources?: ResourceMutationList;
  /** Attribute paths that this Unit needs from upstream Units via NeedsProvides Links. Computed from get-needed and stored on data updates. */
  NeededPaths?: AttributeValue[];
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Sequence number the previous Revision applied. 0 if no live revision. */
  PreviousLiveRevisionNum?: number;
  /** Attribute paths that this Unit provides to downstream Units via NeedsProvides Links. Computed from get-provided and stored on data updates. */
  ProvidedPaths?: AttributeInfo[];
  /** ProviderType identifies which bridge to use in the case that the Target supports multiple ProviderTypes. */
  ProviderType?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** TargetID is the identifier of the target this unit is associated with. This defines where the configuration will be applied. It must be set to a valid Target within the same Space before the Unit can be Applied, Destroyed, Imported, or Refreshed. */
  TargetID?: string;
  /** Bridge option values set per-Unit, merged with the Target's Options when sending to the bridge worker (Target's Options take precedence on overlap). The options must be predefined by the ConfigType in the BridgeWorker. */
  TargetOptions?: {
    [key: string]: string;
  };
  /** ToolchainType specifies the type of toolchain for this unit. Possible values include "Kubernetes/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text", "ConfigHub/YAML". */
  ToolchainType: string;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** Unique identifier for the Organization of the Unit this unit was cloned from, if any. */
  UpstreamOrganizationID?: string;
  /** Sequence number for the Revision of the Unit this unit was cloned from, or 0. This is updated to the upstream Unit's head revision number when the Unit is upgraded. To change this revision number, change the UpstreamLastMergedRevisionNum of the corresponding Link of UpdateType UpgradeUnit from this Unit to the upstream Unit. */
  UpstreamRevisionNum?: number;
  /** Unique identifier for the Space of the Unit this unit was cloned from, if any. */
  UpstreamSpaceID?: string;
  /** Unique identifier for Unit this unit was cloned from, if any. To change or remove the upstream unit, change or delete the corresponding Link of UpdateType UpgradeUnit from this Unit to the upstream Unit. */
  UpstreamUnitID?: string;
  /** A map from gate/warning name to the list of validation results that caused the gate or warning. */
  ValidationResults?: {
    [key: string]: ValidationResultList;
  };
  /** Map from "<trigger slug>/<attribute name>" to the first output Value with that attribute name of the function invocation specified by the Trigger. */
  Values?: {
    [key: string]: string;
  };
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type Binding = {
  /** Shared attribute name that matched the need to the provide */
  AttributeName?: string;
  /** Whether this binding should be automatically updated when the provided value changes; if false, the binding is manual and will not be modified by automatic resolution */
  AutoUpdate?: boolean;
  /** DataType of the bound value */
  DataType?: string;
  /** Whether the provided value comes from the upstream unit's LiveState rather than its Data */
  InLiveState?: boolean;
  /** Resolved path within the needed resource */
  NeededPath?: string;
  NeededResource?: ResourceInfo;
  /** Resolved path within the provided resource */
  ProvidedPath?: string;
  ProvidedResource?: ResourceInfo;
};
export type BindingList = Binding[];
export type PathExpression = {
  /** Data type of the resulting AttributeValue: string, int, or bool. The Expression result (a string) is coerced to this type. */
  DataType?: string;
  /** Expression evaluator: "template" for Go templates or "cel" for CEL */
  Evaluator?: string;
  /** Go template or CEL expression that evaluates to the value to write. Parameters and FunctionContext fields are in scope. */
  Expression?: string;
  /** Names of upstream values referenced by Expression. Each entry must be a legal identifier and must match a Name in UpstreamPaths or UpstreamGetters. */
  Parameters?: string[];
  /** Unresolved path within Resource to write via set-attributes */
  Path?: string;
  Resource?: ResourceInfo;
};
export type ParameterizedFunction = {
  FunctionInvocation?: FunctionInvocation;
  /** Names of upstream values whose values are exposed to string-argument template expansion. Each entry must match a Name in UpstreamPaths or UpstreamGetters. */
  Parameters?: string[];
};
export type NamedFunctionResult = {
  FunctionInvocation?: FunctionInvocation;
  /** Identifier used to reference the value; must be a legal Go and CEL identifier and unique across UpstreamPaths and UpstreamGetters */
  Name?: string;
};
export type NamedPath = {
  /** Identifier used to reference the value from a DownstreamPath Expression; must be a legal Go and CEL identifier */
  Name?: string;
  /** Resolved path within Resource to read via get-paths */
  Path?: string;
  Resource?: ResourceInfo;
};
export type Link = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Automatically update the downstream Unit when the upstream Unit changes. Always treated as true for links with no UpdateType, for backward compatibility. */
  AutoUpdate?: boolean;
  Bindings?: BindingList;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The sequence number of the revision of the downstream unit created by the last merge. */
  DownstreamLastMergedRevisionNum?: number;
  /** Values to write to the downstream Unit when resolving a TransformPaths Link. Each entry evaluates Expression (a Go template or CEL expression, per Evaluator) with the named upstream values (UpstreamPaths and UpstreamGetters) and Space/Unit metadata in scope, coerces the result to DataType, and writes it via set-attributes. Only valid when UpdateType is TransformPaths. */
  DownstreamPaths?: PathExpression[];
  /** Mutating function invocations to run on the downstream Unit. String arguments are template-expanded using the upstream values listed in Parameters (plus Space/Unit metadata) in scope. Each function must be mutating. Worker functions are not supported. Only valid when UpdateType is TransformPaths. */
  DownstreamSetters?: ParameterizedFunction[];
  /** Unique identifier of the downstream (consumer) Unit. Links must be in the same space as the source unit. */
  FromUnitID: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for a Link. */
  LinkID?: string;
  /** Disables the subtraction (override-preservation) step of the merge performed when resolving this Link. When false (the default), the merge subtracts the downstream Unit's local differences from the source patch so they survive the merge. When true, the source patch is applied without subtraction and downstream overrides are preserved only via stored Mutation Predicate values (and WhereMutation). Only meaningful for UpgradeUnit and MergeUnits Links. */
  MergeDisableSubtraction?: boolean;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Unique identifier of the Space of the upstream Unit. */
  ToSpaceID?: string;
  /** Unique identifier of the upstream (producer) Unit. */
  ToUnitID: string;
  /** Identifier of an Invocation whose function is executed on the upstream Unit's data before the result is upserted into the downstream Unit. Only valid when UpdateType is Upsert. The Invocation's ToolchainType must match the upstream Unit's ToolchainType, the function must be non-mutating, and its OutputType must match the downstream Unit's toolchain (currently only Kubernetes/YAML / YAML output). */
  TransformInvocationID?: string;
  /** The ConfigHub operation performed using this Link. Valid values are NeedsProvides, MergeUnits, UpgradeUnit, None, Insert, Upsert, and TransformPaths. If empty, then assumed to be NeedsProvides. UpgradeUnit is like MergeUnits but also keeps the downstream unit's UpstreamRevision fields in sync. Upsert pulls one or more resources produced by the upstream Unit (optionally through a TransformInvocation) and inserts or replaces them in the downstream Unit. TransformPaths reads values from the upstream Unit (UpstreamPaths) and writes expression-derived values to the downstream Unit (DownstreamPaths). */
  UpdateType?: string;
  /** Getter function invocations whose first AttributeValue Value is exposed to DownstreamPaths expressions and DownstreamSetters argument templates by Name, alongside UpstreamPaths. Each function must be non-mutating and produce OutputTypeAttributeValueList. Worker functions are not supported. Only valid when UpdateType is TransformPaths. */
  UpstreamGetters?: NamedFunctionResult[];
  /** The sequence number of the last merged upstream change. When UseLiveState is false, this is the RevisionNum of the last merged revision. When UseLiveState is true, this is the UnitActionNum of the last merged Apply action, since applying the same revision multiple times can produce different LiveState. */
  UpstreamLastMergedRevisionNum?: number;
  /** Values to read from the upstream Unit when resolving a TransformPaths Link. Each NamedPath is read via get-paths and made available to DownstreamPaths expressions by its Name. Only valid when UpdateType is TransformPaths. */
  UpstreamPaths?: NamedPath[];
  /** Take data from the LiveState of the upstream Unit rather than from Data. */
  UseLiveState?: boolean;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Where expression used to filter which Mutations of the downstream Unit can be affected during merge operations. */
  WhereMutation?: string;
  /** Where expression used to select which resources of the upstream Unit should be eligible for propagation to the downstream Unit. */
  WhereResource?: string;
};
export type LinkRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Automatically update the downstream Unit when the upstream Unit changes. Always treated as true for links with no UpdateType, for backward compatibility. */
  AutoUpdate?: boolean;
  Bindings?: BindingList;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The sequence number of the revision of the downstream unit created by the last merge. */
  DownstreamLastMergedRevisionNum?: number;
  /** Values to write to the downstream Unit when resolving a TransformPaths Link. Each entry evaluates Expression (a Go template or CEL expression, per Evaluator) with the named upstream values (UpstreamPaths and UpstreamGetters) and Space/Unit metadata in scope, coerces the result to DataType, and writes it via set-attributes. Only valid when UpdateType is TransformPaths. */
  DownstreamPaths?: PathExpression[];
  /** Mutating function invocations to run on the downstream Unit. String arguments are template-expanded using the upstream values listed in Parameters (plus Space/Unit metadata) in scope. Each function must be mutating. Worker functions are not supported. Only valid when UpdateType is TransformPaths. */
  DownstreamSetters?: ParameterizedFunction[];
  /** The type of entity. */
  EntityType?: string;
  /** Unique identifier of the downstream (consumer) Unit. Links must be in the same space as the source unit. */
  FromUnitID: string;
  /** SHA256 hash of the resolution-relevant Link fields, used to detect changes that require re-resolution. */
  Hash?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for a Link. */
  LinkID?: string;
  /** Disables the subtraction (override-preservation) step of the merge performed when resolving this Link. When false (the default), the merge subtracts the downstream Unit's local differences from the source patch so they survive the merge. When true, the source patch is applied without subtraction and downstream overrides are preserved only via stored Mutation Predicate values (and WhereMutation). Only meaningful for UpgradeUnit and MergeUnits Links. */
  MergeDisableSubtraction?: boolean;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** Unique identifier of the Space of the upstream Unit. */
  ToSpaceID?: string;
  /** Unique identifier of the upstream (producer) Unit. */
  ToUnitID: string;
  /** Identifier of an Invocation whose function is executed on the upstream Unit's data before the result is upserted into the downstream Unit. Only valid when UpdateType is Upsert. The Invocation's ToolchainType must match the upstream Unit's ToolchainType, the function must be non-mutating, and its OutputType must match the downstream Unit's toolchain (currently only Kubernetes/YAML / YAML output). */
  TransformInvocationID?: string;
  /** The ConfigHub operation performed using this Link. Valid values are NeedsProvides, MergeUnits, UpgradeUnit, None, Insert, Upsert, and TransformPaths. If empty, then assumed to be NeedsProvides. UpgradeUnit is like MergeUnits but also keeps the downstream unit's UpstreamRevision fields in sync. Upsert pulls one or more resources produced by the upstream Unit (optionally through a TransformInvocation) and inserts or replaces them in the downstream Unit. TransformPaths reads values from the upstream Unit (UpstreamPaths) and writes expression-derived values to the downstream Unit (DownstreamPaths). */
  UpdateType?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** Getter function invocations whose first AttributeValue Value is exposed to DownstreamPaths expressions and DownstreamSetters argument templates by Name, alongside UpstreamPaths. Each function must be non-mutating and produce OutputTypeAttributeValueList. Worker functions are not supported. Only valid when UpdateType is TransformPaths. */
  UpstreamGetters?: NamedFunctionResult[];
  /** The sequence number of the last merged upstream change. When UseLiveState is false, this is the RevisionNum of the last merged revision. When UseLiveState is true, this is the UnitActionNum of the last merged Apply action, since applying the same revision multiple times can produce different LiveState. */
  UpstreamLastMergedRevisionNum?: number;
  /** Link ID of the link this link was cloned from (if any). */
  UpstreamLinkID?: string;
  /** Organization ID of the link this link was cloned from (if any). */
  UpstreamOrganizationID?: string;
  /** Values to read from the upstream Unit when resolving a TransformPaths Link. Each NamedPath is read via get-paths and made available to DownstreamPaths expressions by its Name. Only valid when UpdateType is TransformPaths. */
  UpstreamPaths?: NamedPath[];
  /** Space ID of the link this link was cloned from (if any). */
  UpstreamSpaceID?: string;
  /** Take data from the LiveState of the upstream Unit rather than from Data. */
  UseLiveState?: boolean;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Where expression used to filter which Mutations of the downstream Unit can be affected during merge operations. */
  WhereMutation?: string;
  /** Where expression used to select which resources of the upstream Unit should be eligible for propagation to the downstream Unit. */
  WhereResource?: string;
};
export type ExtendedLink = {
  Error?: ResponseError;
  FromUnit?: Unit;
  Link?: Link;
  Organization?: Organization;
  Space?: Space;
  ToSpace?: Space;
  ToUnit?: Unit;
  TransformInvocation?: Invocation;
};
export type ExtendedLinkRead = {
  Error?: ResponseError;
  FromUnit?: UnitRead;
  Link?: LinkRead;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  ToSpace?: SpaceRead;
  ToUnit?: UnitRead;
  TransformInvocation?: InvocationRead;
};
export type LinkCreateOrUpdateResponse = {
  Error?: ResponseError;
  Link?: Link;
};
export type LinkCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Link?: LinkRead;
};
export type OrganizationMember = {
  /** Friendly name for the organization member User. */
  DisplayName?: string;
  /** Unique identifier for the External Identity Provider record matching this User. */
  ExternalID?: string;
  /** Unique identifier for the External Identity Provider record matching this organization. */
  ExternalOrganizationID?: string;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** The URL to get the profile avatar picture of the User. */
  ProfilePictureURL?: string;
  /** Unique URL-safe identifier for the organization member User. */
  Slug?: string;
  /** Unique identifier for the organization member User. */
  UserID?: string;
  /** Unique username for a User. Must be unique for all of ConfigHub. */
  Username?: string;
};
export type Revision = {
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers invoking validating functions that did not pass on the configuration data at this Revision. These block Apply operations. */
  ApplyGates?: {
    [key: string]: boolean;
  };
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers with Warn=true invoking validating functions that did not pass on the configuration data at this Revision. These do not block Apply operations. */
  ApplyWarnings?: {
    [key: string]: boolean;
  };
  /** the users that have approved the latest version of the config data for the Unit. */
  ApprovedBy?: Uuid[];
  /** Unique identifier for the ChangeSet to which this Revision belongs. Optional. Revisions are not required to belong to ChangeSets. */
  ChangeSetID?: string;
  /** Deprecated: Use DataHash instead. The CRC32 hash of this revision's data. */
  ContentHash?: number;
  /** The full configuration data for this unit at this revision. */
  Data?: string;
  /** The SHA256 hash of this revision's data, encoded as hexadecimal. */
  DataHash?: string;
  /** User description of the change. It is copied from the LastChangeDescription field of the Unit at the time the change was made that created the Revision. */
  Description?: string;
  /** Time at which the revision was applied, if it was applied. If not applied, the value is "0001-01-01T00:00:00Z". */
  LiveAt?: string;
  MutationSources?: ResourceMutationList;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** Unique identifier for a Revision. */
  RevisionID?: string;
  /** Sequence number for a Revision. */
  RevisionNum?: number;
  /** ConfigHub operation that created this revision. */
  Source?: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** A set (map) of TagIDs of any Tags applied to this Revision. The string values have no particular meaning for now. */
  Tags?: {
    [key: string]: string;
  };
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** User-Agent string if created by an API call. Optional. */
  UserAgent?: string;
  /** UserID if change was made by a user. Automated changes, such as by triggers and resolve, are currently made with the UserID "00000000-0000-0000-0000-000000000000". */
  UserID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type RevisionRead = {
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers invoking validating functions that did not pass on the configuration data at this Revision. These block Apply operations. */
  ApplyGates?: {
    [key: string]: boolean;
  };
  /** A map of "<space slug>/<trigger slug>/<function name>" to true of Triggers with Warn=true invoking validating functions that did not pass on the configuration data at this Revision. These do not block Apply operations. */
  ApplyWarnings?: {
    [key: string]: boolean;
  };
  /** the users that have approved the latest version of the config data for the Unit. */
  ApprovedBy?: Uuid[];
  /** Unique identifier for the ChangeSet to which this Revision belongs. Optional. Revisions are not required to belong to ChangeSets. */
  ChangeSetID?: string;
  /** Deprecated: Use DataHash instead. The CRC32 hash of this revision's data. */
  ContentHash?: number;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** The full configuration data for this unit at this revision. */
  Data?: string;
  /** The SHA256 hash of this revision's data, encoded as hexadecimal. */
  DataHash?: string;
  /** User description of the change. It is copied from the LastChangeDescription field of the Unit at the time the change was made that created the Revision. */
  Description?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Time at which the revision was applied, if it was applied. If not applied, the value is "0001-01-01T00:00:00Z". */
  LiveAt?: string;
  MutationSources?: ResourceMutationList;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** Unique identifier for a Revision. */
  RevisionID?: string;
  /** Sequence number for a Revision. */
  RevisionNum?: number;
  /** ConfigHub operation that created this revision. */
  Source?: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** A set (map) of TagIDs of any Tags applied to this Revision. The string values have no particular meaning for now. */
  Tags?: {
    [key: string]: string;
  };
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** User-Agent string if created by an API call. Optional. */
  UserAgent?: string;
  /** UserID if change was made by a user. Automated changes, such as by triggers and resolve, are currently made with the UserID "00000000-0000-0000-0000-000000000000". */
  UserID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type User = {
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Unique identifier for the External Identity Provider record matching this User. */
  ExternalID?: string;
  /** The URL to get the profile avatar picture of the User. */
  ProfilePictureURL?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a User. */
  UserID?: string;
  /** Unique username for a User. Must be unique for all of Confighub. */
  Username?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type UserRead = {
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Unique identifier for the External Identity Provider record matching this User. */
  ExternalID?: string;
  /** The URL to get the profile avatar picture of the User. */
  ProfilePictureURL?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** Unique identifier for a User. */
  UserID?: string;
  /** Unique username for a User. Must be unique for all of Confighub. */
  Username?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ExtendedRevision = {
  ChangeSet?: ChangeSet;
  Error?: ResponseError;
  Organization?: Organization;
  Revision?: Revision;
  Space?: Space;
  Tags?: Tag[];
  Unit?: Unit;
  User?: User;
};
export type ExtendedRevisionRead = {
  ChangeSet?: ChangeSetRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Revision?: RevisionRead;
  Space?: SpaceRead;
  Tags?: TagRead[];
  Unit?: UnitRead;
  User?: UserRead;
};
export type Trigger = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Function arguments */
  Arguments?: FunctionArgument[] | null;
  /** Unique identifier for a Bridge Worker to execute the function specified by the Trigger. If unspecified, use the builtin function executor. */
  BridgeWorkerID?: string;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** A longer description which explains what the trigger checks and how to fix validation failures. Shown as a pop-up when hovering over an ApplyGate in the UI. */
  Description?: string;
  /** Disabled indicates whether this trigger is currently disabled.
            When disabled, the trigger will not be executed even when matching events occur. */
  Disabled?: boolean;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Event specifies the type of event that will activate this trigger. Valid values are Mutation and PostClone */
  Event: string;
  /** Duration after which a disconnected BridgeWorker's triggers are treated as fail-open. Can only be set when BridgeWorkerID is set. */
  FailOpenAfter?: number | null;
  /** Function name */
  FunctionName?: string;
  /** InvocationID is the identifier of the function to be invoked, if there is a corresponding Invocation. */
  InvocationID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Specifies the source of additional configuration data to pass to functions that need it (e.g., vet-immutable needs LiveRevisionNum data). Uses revision specifier format such as LiveRevisionNum or Before:HeadRevisionNum. */
  OtherDataSource?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** ToolchainType specifies the type of toolchain this trigger works with.
            This determines which configuration formats the trigger can process. */
  ToolchainType: string;
  /** TriggerID uniquely identifies a trigger within the system. */
  TriggerID?: string;
  /** References a Filter entity (with From=Unit) to restrict which Units this Trigger applies to. */
  UnitFilterID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Warn indicates whether this trigger produces ApplyWarnings instead of ApplyGates when its validating function fails. ApplyWarnings are non-blocking. */
  Warn?: boolean;
  /** Restricts which resources within a Unit's configuration data the Trigger's function operates on, using ConfigHub metadata path expressions. */
  WhereResource?: string;
  /** A filter expression to restrict which Units this Trigger applies to. */
  WhereUnit?: string;
};
export type TriggerRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Function arguments */
  Arguments?: FunctionArgument[] | null;
  /** Unique identifier for a Bridge Worker to execute the function specified by the Trigger. If unspecified, use the builtin function executor. */
  BridgeWorkerID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** A longer description which explains what the trigger checks and how to fix validation failures. Shown as a pop-up when hovering over an ApplyGate in the UI. */
  Description?: string;
  /** Disabled indicates whether this trigger is currently disabled.
            When disabled, the trigger will not be executed even when matching events occur. */
  Disabled?: boolean;
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Event specifies the type of event that will activate this trigger. Valid values are Mutation and PostClone */
  Event: string;
  /** Duration after which a disconnected BridgeWorker's triggers are treated as fail-open. Can only be set when BridgeWorkerID is set. */
  FailOpenAfter?: number | null;
  /** Function name */
  FunctionName?: string;
  /** SHA256 hash of the trigger's specification fields, used to detect changes. */
  Hash?: string;
  /** InvocationID is the identifier of the function to be invoked, if there is a corresponding Invocation. */
  InvocationID?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Specifies the source of additional configuration data to pass to functions that need it (e.g., vet-immutable needs LiveRevisionNum data). Uses revision specifier format such as LiveRevisionNum or Before:HeadRevisionNum. */
  OtherDataSource?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** ToolchainType specifies the type of toolchain this trigger works with.
            This determines which configuration formats the trigger can process. */
  ToolchainType: string;
  /** TriggerID uniquely identifies a trigger within the system. */
  TriggerID?: string;
  /** References a Filter entity (with From=Unit) to restrict which Units this Trigger applies to. */
  UnitFilterID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** Validating indicates whether this is a validating function (true) or not (false).
            When false, the function can be either mutating (modifying configuration) or readonly returning an AttributeValueList (extracting values without modification).
            Validating functions check configuration validity without modifying it.
            This value is returned by ConfigHub based on the corresponding property of the specified function. */
  Validating?: boolean;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Warn indicates whether this trigger produces ApplyWarnings instead of ApplyGates when its validating function fails. ApplyWarnings are non-blocking. */
  Warn?: boolean;
  /** Restricts which resources within a Unit's configuration data the Trigger's function operates on, using ConfigHub metadata path expressions. */
  WhereResource?: string;
  /** A filter expression to restrict which Units this Trigger applies to. */
  WhereUnit?: string;
};
export type ExtendedSpace = {
  AttributeFilter?: Filter;
  Attributes?: Attribute[];
  Error?: ResponseError;
  GatedUnitCount?: number;
  IncompleteApplyUnitCount?: number;
  Organization?: Organization;
  Space?: Space;
  TargetCountByToolchainType?: {
    [key: string]: number;
  } | null;
  TotalAttributeCount?: number;
  TotalBridgeWorkerCount?: number;
  TotalChangeSetCount?: number;
  TotalFilterCount?: number;
  TotalInvocationCount?: number;
  TotalLinkCount?: number;
  TotalTagCount?: number;
  TotalUnitCount?: number;
  TotalViewCount?: number;
  TriggerCountByEventType?: {
    [key: string]: number;
  } | null;
  TriggerFilter?: Filter;
  Triggers?: Trigger[];
  UnappliedUnitCount?: number;
  UnapprovedUnitCount?: number;
  UnlinkedUnitCount?: number;
  UpgradableUnitCount?: number;
};
export type ExtendedSpaceRead = {
  AttributeFilter?: FilterRead;
  Attributes?: AttributeRead[];
  Error?: ResponseError;
  GatedUnitCount?: number;
  IncompleteApplyUnitCount?: number;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  TargetCountByToolchainType?: {
    [key: string]: number;
  } | null;
  TotalAttributeCount?: number;
  TotalBridgeWorkerCount?: number;
  TotalChangeSetCount?: number;
  TotalFilterCount?: number;
  TotalInvocationCount?: number;
  TotalLinkCount?: number;
  TotalTagCount?: number;
  TotalUnitCount?: number;
  TotalViewCount?: number;
  TriggerCountByEventType?: {
    [key: string]: number;
  } | null;
  TriggerFilter?: FilterRead;
  Triggers?: TriggerRead[];
  UnappliedUnitCount?: number;
  UnapprovedUnitCount?: number;
  UnlinkedUnitCount?: number;
  UpgradableUnitCount?: number;
};
export type BridgeWorkerStatus = {
  /** Unique identifier for the Bridge Worker. */
  BridgeWorkerID?: string;
  /** Slug for the Bridge Worker. */
  BridgeWorkerSlug?: string;
  /** BridgeWorkerStatusID is the unique identifier for the bridge worker status entry. */
  BridgeWorkerStatusID?: string;
  /** IPAddress is the IP address from which the bridge worker is connecting. */
  IPAddress?: string;
  /** OrganizationID is the unique identifier of the organization the bridge worker belongs to. */
  OrganizationID?: string;
  /** The timestamp when the bridge worker last responded in "2023-01-01T12:00:00Z" format. */
  SeenAt?: string;
  /** SpaceID is the unique identifier of the space the bridge worker belongs to. */
  SpaceID?: string;
  /** Status indicates the current status of the bridge worker. Possible values include Connected, Disconnected, ActionSent, ActionResultReceived. */
  Status?: string;
};
export type ExtendedTag = {
  ChangeSet?: ChangeSet;
  Error?: ResponseError;
  Organization?: Organization;
  Space?: Space;
  Tag?: Tag;
};
export type ExtendedTagRead = {
  ChangeSet?: ChangeSetRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  Tag?: TagRead;
};
export type TargetConfigType = {
  /** Configuration toolchain and format of the LiveState for this bridge; required in order to invoke functions on LiveState */
  LiveStateType?: string;
  Options?: {
    [key: string]: string;
  };
  /** Type identifying a bridge implementation supported by the worker */
  ProviderType?: string;
  /** Configuration toolchain and format implemented by this bridge of the worker */
  ToolchainType?: string;
};
export type Target = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Identifier used by the Bridge to refer to discovered/enabled Target credentials and coordinates. */
  BridgeHandle?: string;
  /** Unique identifier for a Bridge Worker associated with the Target. */
  BridgeWorkerID: string;
  /** ConfigTypes (ToolchainType, ProviderType, LiveStateType tuples) supported by this Target. */
  ConfigTypes?: TargetConfigType[];
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** Facts are properties of the infrastructure this Target represents (e.g. a Kubernetes cluster), as a flat string-to-string map. Collected facts use reserved key prefixes such as "Cluster." and are (re)written by fact collection; all other keys are user-defined custom facts. Facts can be referenced in where queries, e.g. Facts.Cluster.KubernetesVersion = '1.31.2'. */
  Facts?: {
    [key: string]: string;
  };
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** LiveStateType specifies the first/default configuration toolchain and format of the LiveState for the bridge corresponding to this Target. Possible values include "Kubernetes/YAML" and "ConfigHub/YAML". */
  LiveStateType?: string;
  /** Bridge option values for the first ProviderType. The options must be predefined by the ConfigType in the BridgeWorker. */
  Options?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Deprecated. Parameters contains toolchain-type and/or provider-type-specific parameters in JSON format.
    
    For ProviderType: Kubernetes (ToolchainType: Kubernetes/YAML)
    The Parameters object may contain the following fields:
    - "KubeContext" (string): The name of the Kubernetes context (from "~/.kube/config") to use. (Not typically needed if running in-cluster).
     */
  Parameters?: string;
  Permissions?: Permissions;
  /** ProviderType specifies the first/default cloud or infrastructure provider for this target, such as "Kubernetes". */
  ProviderType: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Unique identifier for a Target. */
  TargetID?: string;
  /** ToolchainType specifies the type of the first/default toolchain supported by this Target. Possible values include "Kubernetes/YAML", "ConfigHub/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text". */
  ToolchainType: string;
  /** Reference to a Filter entity used to identify Triggers that should be invoked on Units this Target is attached to. The Filter's From field must be set to 'Trigger'. */
  TriggerFilterID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Filter expression to identify Triggers that should be invoked on Units this Target is attached to. The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  WhereTrigger?: string;
};
export type TargetRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Identifier used by the Bridge to refer to discovered/enabled Target credentials and coordinates. */
  BridgeHandle?: string;
  /** Unique identifier for a Bridge Worker associated with the Target. */
  BridgeWorkerID: string;
  /** ConfigTypes (ToolchainType, ProviderType, LiveStateType tuples) supported by this Target. */
  ConfigTypes?: TargetConfigType[];
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** Facts are properties of the infrastructure this Target represents (e.g. a Kubernetes cluster), as a flat string-to-string map. Collected facts use reserved key prefixes such as "Cluster." and are (re)written by fact collection; all other keys are user-defined custom facts. Facts can be referenced in where queries, e.g. Facts.Cluster.KubernetesVersion = '1.31.2'. */
  Facts?: {
    [key: string]: string;
  };
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** LiveStateType specifies the first/default configuration toolchain and format of the LiveState for the bridge corresponding to this Target. Possible values include "Kubernetes/YAML" and "ConfigHub/YAML". */
  LiveStateType?: string;
  /** Bridge option values for the first ProviderType. The options must be predefined by the ConfigType in the BridgeWorker. */
  Options?: {
    [key: string]: string;
  };
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Deprecated. Parameters contains toolchain-type and/or provider-type-specific parameters in JSON format.
    
    For ProviderType: Kubernetes (ToolchainType: Kubernetes/YAML)
    The Parameters object may contain the following fields:
    - "KubeContext" (string): The name of the Kubernetes context (from "~/.kube/config") to use. (Not typically needed if running in-cluster).
     */
  Parameters?: string;
  Permissions?: Permissions;
  /** ProviderType specifies the first/default cloud or infrastructure provider for this target, such as "Kubernetes". */
  ProviderType: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** Unique identifier for a Target. */
  TargetID?: string;
  /** ToolchainType specifies the type of the first/default toolchain supported by this Target. Possible values include "Kubernetes/YAML", "ConfigHub/YAML", "AppConfig/Properties", "AppConfig/YAML", "AppConfig/TOML", "AppConfig/INI", "AppConfig/JSON", "AppConfig/Env", "AppConfig/Text". */
  ToolchainType: string;
  /** Reference to a Filter entity used to identify Triggers that should be invoked on Units this Target is attached to. The Filter's From field must be set to 'Trigger'. */
  TriggerFilterID?: string;
  TriggerHash?: string;
  /** List of Trigger IDs that match the WhereTrigger and/or TriggerFilterID criteria. (readonly) */
  TriggerIDs?: Uuid[];
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** Filter expression to identify Triggers that should be invoked on Units this Target is attached to. The specified string is an expression for the purpose of filtering
    the list of Triggers returned. The expression syntax was inspired by SQL.
    It supports conjunctions using `AND` of relational expressions of the form *attribute*
    *operator* *attribute_or_literal*. The attribute names are case-sensitive and PascalCase,
    as in the JSON encoding.
    Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `~*`, `!~`, `!~*`, `IN`, `NOT IN`.
    String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards,
    `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching.
    String regex operators: `~` for regex matching, `~*` for case-insensitive regex,
    `!~` and `!~*` for regex not matching (case-sensitive and insensitive).
    Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`.
    UUIDs and boolean attributes support equality and inequality only.
    UUID and time literals must be quoted as string literals.
    String literals are quoted with single quotes, such as `'string'`.
    Time literals use the same form as when serialized as JSON,
    such as: `CreatedAt > '2025-02-18T23:16:34'`.
    Integer and boolean literals are also supported for attributes of those types.
    Arrays support the `?` operator to to match any element of the array,
    as in `ApprovedBy ? '7c61626f-ddbe-41af-93f6-b69f4ab6d308'`.
    Arrays can perform LEN() to check for length, as in `LEN(ApprovedBy) > 0`.
    Map support the dot notation to specify a particular map key, as in `Labels.tier = 'Backend'`.
    Maps support `IS NULL` and `IS NOT NULL` with dot notation to check for key absence or presence,
    as in `Labels.tier IS NULL` (key doesn't exist) or `Labels.tier IS NOT NULL` (key exists).
    Comparison results can be tested with `IS TRUE`, `IS FALSE`, `IS NOT TRUE`, and `IS NOT FALSE`.
    These are useful for nullable columns: `MergeSourceID = '<uuid>' IS NOT FALSE` matches rows where MergeSourceID equals the value OR is NULL.
    The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses,
    such as `Slug IN ('slugone', 'slugtwo')` or `Labels.environment IN ('prod', 'staging')`.
    Conjunctions are supported using the `AND` operator.
    An example conjunction is:
    `CreatedAt >= '2025-01-07' AND Slug = 'test' AND Labels.mykey = 'myvalue'`.
    
    Supported attributes for filtering on Trigger: Annotations, BridgeWorkerID, CreatedAt, DeleteGates, Description, Disabled, DisplayName, Event, FunctionName, Hash, InvocationID, Labels, OrganizationID, OtherDataSource, Slug, SpaceID, ToolchainType, TriggerID, UnitFilterID, UpdatedAt, Validating, Warn, WhereResource, WhereUnit.
    
    The whole string must be query-encoded. */
  WhereTrigger?: string;
};
export type ExtendedTarget = {
  BridgeWorker?: BridgeWorker;
  Error?: ResponseError;
  Organization?: Organization;
  Space?: Space;
  Target?: Target;
  TriggerFilter?: Filter;
  Triggers?: Trigger[];
};
export type ExtendedTargetRead = {
  BridgeWorker?: BridgeWorkerRead;
  Error?: ResponseError;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  Target?: TargetRead;
  TriggerFilter?: FilterRead;
  Triggers?: TriggerRead[];
};
export type ExtendedTrigger = {
  BridgeWorker?: BridgeWorker;
  Error?: ResponseError;
  Invocation?: Invocation;
  Organization?: Organization;
  Space?: Space;
  Trigger?: Trigger;
  UnitFilter?: Filter;
};
export type ExtendedTriggerRead = {
  BridgeWorker?: BridgeWorkerRead;
  Error?: ResponseError;
  Invocation?: InvocationRead;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  Trigger?: TriggerRead;
  UnitFilter?: FilterRead;
};
export type ResourceInfoType2 = {
  /** Category of configuration element represented in the configuration data; Kubernetes resources are of category Resource, and application configuration files are of category AppConfig */
  ResourceCategory?: string;
  /** Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID */
  ResourceName?: string;
  /** Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip */
  ResourceNameStableCore?: string;
  /** Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind) */
  ResourceType?: string;
};
export type Mutation = {
  FunctionInvocation?: FunctionInvocation;
  /** InvocationID is the identifier of the function invoked, if there is a corresponding Invocation. */
  InvocationID?: string;
  /** LinkID is the unique identifier of the link if the change was made due to resolving a link. */
  LinkID?: string;
  /** MergeBaseRevisionNum is the sequence number of the revision preceding merged changes, if the change was due to a merge operation. */
  MergeBaseRevisionNum?: number;
  /** MergeEndRevisionNum is the sequence number of the revision ending merged changes, if the change was due to a merge operation. */
  MergeEndRevisionNum?: number;
  /** MergeSourceID is the unique identifier of the unit if the change was made due to merging from another unit, including for clone and upgrade. */
  MergeSourceID?: string;
  /** Unique identifier for a Mutation. */
  MutationID?: string;
  /** Sequence number for the Mutation. */
  MutationNum?: number;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** ProvidedPath is the path of the provided value used to satisfy a needed value if the change was made due to resolving a link. */
  ProvidedPath?: string;
  ProvidedResource?: ResourceInfoType2;
  /** Sequence number of the restored revision, if the change was due to a restore operation. */
  RestoredRevisionNum?: number;
  /** Unique identifier of the corresponding Revision. */
  RevisionID?: string;
  /** Sequence number of the corresponding Revision. */
  RevisionNum?: number;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** User-defined category for the Mutation. The prefix 'ConfigHub' is reserved. */
  Subgroup?: string;
  /** TriggerID is the unique identifier of the trigger if the change was made by a trigger. */
  TriggerID?: string;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** Sequence number of the upstream revision the unit was upgraded from, if the change was due to an upgrade operation. */
  UpgradedFromUpstreamRevisionNum?: number;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type MutationRead = {
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** The type of entity. */
  EntityType?: string;
  FunctionInvocation?: FunctionInvocation;
  /** InvocationID is the identifier of the function invoked, if there is a corresponding Invocation. */
  InvocationID?: string;
  /** LinkID is the unique identifier of the link if the change was made due to resolving a link. */
  LinkID?: string;
  /** MergeBaseRevisionNum is the sequence number of the revision preceding merged changes, if the change was due to a merge operation. */
  MergeBaseRevisionNum?: number;
  /** MergeEndRevisionNum is the sequence number of the revision ending merged changes, if the change was due to a merge operation. */
  MergeEndRevisionNum?: number;
  /** MergeSourceID is the unique identifier of the unit if the change was made due to merging from another unit, including for clone and upgrade. */
  MergeSourceID?: string;
  /** Unique identifier for a Mutation. */
  MutationID?: string;
  /** Sequence number for the Mutation. */
  MutationNum?: number;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** ProvidedPath is the path of the provided value used to satisfy a needed value if the change was made due to resolving a link. */
  ProvidedPath?: string;
  ProvidedResource?: ResourceInfoType2;
  /** Sequence number of the restored revision, if the change was due to a restore operation. */
  RestoredRevisionNum?: number;
  /** Unique identifier of the corresponding Revision. */
  RevisionID?: string;
  /** Sequence number of the corresponding Revision. */
  RevisionNum?: number;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** User-defined category for the Mutation. The prefix 'ConfigHub' is reserved. */
  Subgroup?: string;
  /** TriggerID is the unique identifier of the trigger if the change was made by a trigger. */
  TriggerID?: string;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** Sequence number of the upstream revision the unit was upgraded from, if the change was due to an upgrade operation. */
  UpgradedFromUpstreamRevisionNum?: number;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type UnitEvent = {
  Action?: ActionType;
  /** BridgeWorkerID is the ID of the bridge worker that performed this action. This field is populated from the Target's BridgeWorkerID when the event is created. */
  BridgeWorkerID?: string;
  Message?: string;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** QueuedOperationID is the unique identifier for the corresponding queued operation. */
  QueuedOperationID?: string;
  ResourceStatuses?: ResourceStatusMap;
  Result?: ActionResultType;
  RevisionNum?: number;
  /** Unique identifier for a space. */
  SpaceID?: string;
  StartedAt?: string;
  Status?: ActionStatusType;
  TerminatedAt?: string | null;
  UnitEventID?: string;
  /** Sequence number for this unit event. */
  UnitEventNum?: number;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type UnitEventRead = {
  Action?: ActionType;
  /** BridgeWorkerID is the ID of the bridge worker that performed this action. This field is populated from the Target's BridgeWorkerID when the event is created. */
  BridgeWorkerID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** The type of entity. */
  EntityType?: string;
  Message?: string;
  /** Unique identifier for an Organization. */
  OrganizationID?: string;
  /** QueuedOperationID is the unique identifier for the corresponding queued operation. */
  QueuedOperationID?: string;
  ResourceStatuses?: ResourceStatusMap;
  Result?: ActionResultType;
  RevisionNum?: number;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  StartedAt?: string;
  Status?: ActionStatusType;
  TerminatedAt?: string | null;
  UnitEventID?: string;
  /** Sequence number for this unit event. */
  UnitEventNum?: number;
  /** Unique identifier for a Unit. */
  UnitID?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
};
export type ResourceStatusSummary = {
  /** Number of resources with Readiness=Failed */
  Failed?: number;
  /** Earliest UpdatedAt timestamp across all resources */
  FirstUpdatedAt?: string | null;
  /** Most recent UpdatedAt timestamp across all resources */
  LastUpdatedAt?: string | null;
  /** Number of resources with Readiness=InProgress */
  Progressing?: number;
  /** Number of resources with Readiness=Ready */
  Ready?: number;
  /** Number of resources with SyncStatus=Synced */
  Synced?: number;
  /** Total number of resources in the unit */
  Total?: number;
};
export type UnitStatus = {
  Action?: ActionType;
  ActionResult?: ActionResultType;
  ActionStartedAt?: string | null;
  ActionTerminatedAt?: string | null;
  Drift?: string;
  ResourceStatusSummary?: ResourceStatusSummary;
  Status?: string;
  SyncStatus?: string;
};
export type AttributeSelector = {
  Path?: string;
  WhereResource?: string;
};
export type ColumnSource = {
  DataExpression?: string;
  DataPath?: AttributeSelector;
  MetadataAttribute?: string;
  MetadataExpression?: string;
};
export type Column = {
  ColumnSource?: ColumnSource;
  ColumnType?: string;
  DataType?: string;
  GroupBy?: boolean;
  Name: string;
  OrderByDirection?: string;
};
export type View = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Columns to display, in order. (optional) */
  Columns?: Column[];
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** FilterID identifies a filter. At least one of FilterID or Of must be specified. (optional) */
  FilterID?: string;
  /** Column to group by (optional). */
  GroupBy?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Entity type to view (e.g., Unit, Space). At least one of FilterID or Of must be specified. If both are specified, Of must match Filter.From. (optional) */
  Of?: string;
  /** Column to sort by. (optional) */
  OrderBy?: string;
  /** Columnn sort order, ASC or DESC. Default is ASC. Only should be specified if OrderBy is specified. (optional) */
  OrderByDirection?: string;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** ViewID uniquely identifies a view within the system. */
  ViewID?: string;
};
export type ViewRead = {
  /** An optional map of Annotation key/value pairs for tools to attach information to entities. */
  Annotations?: {
    [key: string]: string;
  };
  /** Columns to display, in order. (optional) */
  Columns?: Column[];
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** An auto-incrementing sequence number used for pagination. */
  CursorID?: number;
  /** An optional set of gates that, if any is present, will block deletion. */
  DeleteGates?: {
    [key: string]: boolean;
  };
  /** Friendly name for the entity. */
  DisplayName?: string;
  /** The type of entity. */
  EntityType?: string;
  /** FilterID identifies a filter. At least one of FilterID or Of must be specified. (optional) */
  FilterID?: string;
  /** Column to group by (optional). */
  GroupBy?: string;
  /** An optional map of Label key/value pairs to specify identifying attributes of entities for the purpose of grouping and filtering them. */
  Labels?: {
    [key: string]: string;
  };
  /** Entity type to view (e.g., Unit, Space). At least one of FilterID or Of must be specified. If both are specified, Of must match Filter.From. (optional) */
  Of?: string;
  /** Column to sort by. (optional) */
  OrderBy?: string;
  /** Columnn sort order, ASC or DESC. Default is ASC. Only should be specified if OrderBy is specified. (optional) */
  OrderByDirection?: string;
  /** Unique identifier for an organization. */
  OrganizationID?: string;
  /** Unique URL-safe identifier for the entity. */
  Slug: string;
  /** Unique identifier for a space. */
  SpaceID?: string;
  /** Slug of the Space this entity belongs to. (readonly) */
  SpaceSlug?: string;
  /** The timestamp when the entity was last updated in "2023-01-01T12:00:00Z" format. */
  UpdatedAt?: string;
  /** An entity-specific sequence number used for optimistic concurrency control. The value read must be sent in calls to Update. */
  Version?: number;
  /** ViewID uniquely identifies a view within the system. */
  ViewID?: string;
};
export type ViewColumn = {
  Name?: string;
  Value?: string;
};
export type ExtendedUnit = {
  /** the users that have approved the latest revision of the config data. */
  ApprovedBy?: User[];
  BridgeWorker?: BridgeWorker;
  ChangeSet?: ChangeSet;
  Error?: ResponseError;
  FromLink?: Link[];
  HeadMutation?: Mutation;
  HeadRevision?: Revision;
  LastAppliedRevision?: Revision;
  LatestUnitEvent?: UnitEvent;
  LiveRevision?: Revision;
  Organization?: Organization;
  PreviousLiveRevision?: Revision;
  Space?: Space;
  Target?: Target;
  Unit?: Unit;
  UnitStatus?: UnitStatus;
  UpstreamSpace?: Space;
  UpstreamUnit?: Unit;
  View?: View;
  ViewColumns?: ViewColumn[];
};
export type ExtendedUnitRead = {
  /** the users that have approved the latest revision of the config data. */
  ApprovedBy?: UserRead[];
  BridgeWorker?: BridgeWorkerRead;
  ChangeSet?: ChangeSetRead;
  Error?: ResponseError;
  FromLink?: LinkRead[];
  HeadMutation?: MutationRead;
  HeadRevision?: RevisionRead;
  LastAppliedRevision?: RevisionRead;
  LatestUnitEvent?: UnitEventRead;
  LiveRevision?: RevisionRead;
  Organization?: OrganizationRead;
  PreviousLiveRevision?: RevisionRead;
  Space?: SpaceRead;
  Target?: TargetRead;
  Unit?: UnitRead;
  UnitStatus?: UnitStatus;
  UpstreamSpace?: SpaceRead;
  UpstreamUnit?: UnitRead;
  View?: ViewRead;
  ViewColumns?: ViewColumn[];
};
export type ApproveResponse = {
  Error?: ResponseError;
  Message?: string;
  Unit?: Unit;
};
export type ApproveResponseRead = {
  Error?: ResponseError;
  Message?: string;
  Unit?: UnitRead;
};
export type UnitExtended = {
  Action?: ActionType;
  ActionResult?: ActionResultType;
  ActionStartedAt?: string | null;
  ActionTerminatedAt?: string | null;
  ApprovedByUsers?: string[] | null;
  Drift?: string;
  FromLinks?: Link[] | null;
  ResourceStatusSummary?: ResourceStatusSummary;
  Status?: string;
  SyncStatus?: string;
  ToLinks?: Link[] | null;
  Unit?: Unit;
};
export type UnitExtendedRead = {
  Action?: ActionType;
  ActionResult?: ActionResultType;
  ActionStartedAt?: string | null;
  ActionTerminatedAt?: string | null;
  ApprovedByUsers?: string[] | null;
  Drift?: string;
  FromLinks?: LinkRead[] | null;
  ResourceStatusSummary?: ResourceStatusSummary;
  Status?: string;
  SyncStatus?: string;
  ToLinks?: LinkRead[] | null;
  Unit?: UnitRead;
};
export type ImportFilter = {
  /** Operator specifies how to apply the filter (include, exclude, equals, contains, matches) */
  Operator?: string;
  /** Type specifies the filter type (namespace, label, resource_type, etc.) */
  Type?: string;
  /** Values specifies the filter values */
  Values?: string[];
};
export type ImportOptions = {
  [key: string]: any;
};
export type ResourceInfoList = ResourceInfo[];
export type ImportRequest = {
  /** List of ImportFilter expression clauses. Mutually exclusive with Where. */
  Filters?: ImportFilter[];
  Options?: ImportOptions;
  ResourceInfoList?: ResourceInfoList;
  /** Where specifies a unified resource filter expression for import resources and options. It uses SQL-inspired syntax, similar to the where-filter function. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. Kubernetes-specific filters include import.include_system for system namespaces like kube-system, import.include_cluster for cluster-scoped resources like ClusterRole, and import.include_custom for custom resource types. */
  Where?: string;
};
export type ExtendedMutation = {
  Error?: ResponseError;
  Invocation?: Invocation;
  Link?: Link;
  MergeSource?: Unit;
  Mutation?: Mutation;
  Organization?: Organization;
  Revision?: Revision;
  Space?: Space;
  Trigger?: Trigger;
  Unit?: Unit;
};
export type ExtendedMutationRead = {
  Error?: ResponseError;
  Invocation?: InvocationRead;
  Link?: LinkRead;
  MergeSource?: UnitRead;
  Mutation?: MutationRead;
  Organization?: OrganizationRead;
  Revision?: RevisionRead;
  Space?: SpaceRead;
  Trigger?: TriggerRead;
  Unit?: UnitRead;
};
export type UnitPredicatesResponse = {
  Error?: ResponseError;
  MutationSources?: ResourceMutationList;
};
export type ResourcePredicates = {
  /** Map of resolved path to its new Predicate value: true = eligible to be overwritten by a merge, false = protected local override */
  Predicates?: {
    [key: string]: boolean;
  } | null;
  Resource?: ResourceInfo;
};
export type UnitPredicatesRequest = {
  /** Per-resource Predicate edits to apply to the Unit's MutationSources */
  ResourcePredicates?: ResourcePredicates[] | null;
};
export type UnitAction = {
  Action?: ActionType;
  BridgeState?: string;
  /** BridgeWorkerID is the unique identifier of the bridge worker that will process this operation. */
  BridgeWorkerID?: string;
  /** The timestamp when the entity was created in "2023-01-01T12:00:00Z" format. */
  CreatedAt?: string;
  /** The result of a dry-run Data-changing action like refresh and import, where the data is not stored in the Unit. */
  Data?: string;
  /** Dependencies contains the list of operation IDs that this operation depends on. Operations will not be delivered until all dependencies are completed. */
  Dependencies?: Uuid[] | null;
  /** The drift reconciliation mode for the unit at the time of the operation. */
  DriftReconciliationMode?: string;
  /** DryRun indicates whether the action is a dry run. */
  DryRun?: boolean;
  /** Error details returned by the worker. */
  ErrorDetails?: ErrorItem[];
  /** ExtraParams contains additional parameters for the operation in string format. */
  ExtraParams?: string;
  LiveData?: string;
  LiveState?: string;
  /** OrganizationID is the unique identifier of the organization this operation belongs to. */
  OrganizationID?: string;
  /** QueuedOperationID is the unique identifier for the queued unit action. */
  QueuedOperationID?: string;
  /** RevisionNum is the revision number this operation was performed on. */
  RevisionNum?: number;
  /** SpaceID is the unique identifier of the space of the unit this operation is performed on. */
  SpaceID?: string;
  /** Status indicates the current status of the unit action. v2 statuses: Initializing (being set up), Pending (waiting), Delivered (sent to worker), Progressing (being processed), Completed (success), Failed (error). v1 compatibility: 'pending' = Pending, 'delivered' = Completed (legacy 'delivered' meant work done). */
  Status?:
    | 'Initializing'
    | 'Pending'
    | 'Delivered'
    | 'Progressing'
    | 'Completed'
    | 'Failed'
    | 'Aborted'
    | 'Canceled'
    | 'pending'
    | 'delivered';
  /** TargetID is the unique identifier of the target this operation is directed to. */
  TargetID?: string;
  /** UnitActionNum is the sequence number of this unit action. */
  UnitActionNum?: number;
  /** UnitID is the unique identifier of the unit this operation is performed on. */
  UnitID?: string;
  /** User-Agent string of the API call. */
  UserAgent?: string;
  /** UserID of the user the action was performed by. */
  UserID?: string;
  /** Whether the user who requested the action is a bot user. */
  UserIsBot?: boolean;
  /** Organization-level role of the user who requested the action. */
  UserRole?: string;
  /** An entity-specific sequence number used for optimistic concurrency control.
    The value read must be sent in calls to Update. */
  Version?: number;
};
export type ExtendedView = {
  Error?: ResponseError;
  Filter?: Filter;
  Organization?: Organization;
  Space?: Space;
  View?: View;
};
export type ExtendedViewRead = {
  Error?: ResponseError;
  Filter?: FilterRead;
  Organization?: OrganizationRead;
  Space?: SpaceRead;
  View?: ViewRead;
};
export type TagCreateOrUpdateResponse = {
  Error?: ResponseError;
  Tag?: Tag;
};
export type TagCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Tag?: TagRead;
};
export type TargetCreateOrUpdateResponse = {
  Error?: ResponseError;
  Target?: Target;
};
export type TargetCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Target?: TargetRead;
};
export type TriggerCreateOrUpdateResponse = {
  Error?: ResponseError;
  Trigger?: Trigger;
};
export type TriggerCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  Trigger?: TriggerRead;
};
export type MutationConflict = {
  /** Path of the mutation; empty for resource-level conflicts */
  Path?: string;
  /** Why the mutation was dropped */
  Reason?: string;
  Resource?: ResourceInfo;
  Source?: MutationInfo;
  Target?: MutationInfo;
  /** ID of the other unit involved in the conflict (upstream for upgrade/merge, link target for resolve) */
  UnitID?: string;
};
export type MutationConflictList = MutationConflict[];
export type UnitCreateOrUpdateResponse = {
  Conflicts?: MutationConflictList;
  Error?: ResponseError;
  Links?: LinkCreateOrUpdateResponse[];
  Unit?: Unit;
};
export type UnitCreateOrUpdateResponseRead = {
  Conflicts?: MutationConflictList;
  Error?: ResponseError;
  Links?: LinkCreateOrUpdateResponseRead[];
  Unit?: UnitRead;
};
export type UnitActionResponse = {
  Action?: QueuedOperation;
  Error?: ResponseError;
};
export type UnitTagResponse = {
  Error?: ResponseError;
  Message?: string;
};
export type UnitTagRequest = {
  /** Which Unit revision to tag: 'HeadRevisionNum', 'LiveRevisionNum', 'LastAppliedRevisionNum', 'PreviousLiveRevisionNum', or 'Remove' to remove the tag from the unit */
  Revision?: string;
  TagID?: string;
};
export type ViewCreateOrUpdateResponse = {
  Error?: ResponseError;
  View?: View;
};
export type ViewCreateOrUpdateResponseRead = {
  Error?: ResponseError;
  View?: ViewRead;
};
export const {
  useBulkDeleteSpacesMutation,
  useBulkPatchSpacesMutation,
  useBulkCreateSpacesMutation,
  useBulkDeleteAttributesMutation,
  useListAllAttributesQuery,
  useLazyListAllAttributesQuery,
  useBulkPatchAttributesMutation,
  useBulkCreateAttributesMutation,
  useBulkDeleteBridgeWorkersMutation,
  useListAllBridgeWorkersQuery,
  useLazyListAllBridgeWorkersQuery,
  useBulkPatchBridgeWorkersMutation,
  useCreateActionResultMutation,
  useGetSelfQuery,
  useLazyGetSelfQuery,
  useListQueuedOperationsQuery,
  useLazyListQueuedOperationsQuery,
  useGetQueuedOperationQuery,
  useLazyGetQueuedOperationQuery,
  useStreamBridgeWorkerMutation,
  useUserCreateActionResultMutation,
  useBulkDeleteChangeSetsMutation,
  useListAllChangeSetsQuery,
  useLazyListAllChangeSetsQuery,
  useBulkPatchChangeSetsMutation,
  useBulkCreateChangeSetsMutation,
  useBulkDeleteFiltersMutation,
  useListAllFiltersQuery,
  useLazyListAllFiltersQuery,
  useBulkPatchFiltersMutation,
  useBulkCreateFiltersMutation,
  useListOrgFunctionsQuery,
  useLazyListOrgFunctionsQuery,
  useInvokeFunctionsOnOrgMutation,
  useApiInfoQuery,
  useLazyApiInfoQuery,
  useBulkDeleteInvocationsMutation,
  useListAllInvocationsQuery,
  useLazyListAllInvocationsQuery,
  useBulkPatchInvocationsMutation,
  useBulkCreateInvocationsMutation,
  useBulkDeleteLinksMutation,
  useSearchListLinksQuery,
  useLazySearchListLinksQuery,
  useBulkPatchLinksMutation,
  useBulkCreateLinksMutation,
  useGetMeQuery,
  useLazyGetMeQuery,
  useListOrganizationsQuery,
  useLazyListOrganizationsQuery,
  useCreateOrganizationMutation,
  useDeleteOrganizationMutation,
  useGetOrganizationQuery,
  useLazyGetOrganizationQuery,
  useUpdateOrganizationMutation,
  useListOrganizationMembersQuery,
  useLazyListOrganizationMembersQuery,
  useCreateOrganizationMemberMutation,
  useDeleteOrganizationMemberMutation,
  useGetOrganizationMemberQuery,
  useLazyGetOrganizationMemberQuery,
  useListAllRevisionsQuery,
  useLazyListAllRevisionsQuery,
  useListSpacesQuery,
  useLazyListSpacesQuery,
  useCreateSpaceMutation,
  useDeleteSpaceMutation,
  useGetSpaceQuery,
  useLazyGetSpaceQuery,
  usePatchSpaceMutation,
  useUpdateSpaceMutation,
  useListAttributesQuery,
  useLazyListAttributesQuery,
  useCreateAttributeMutation,
  useDeleteAttributeMutation,
  useGetAttributeQuery,
  useLazyGetAttributeQuery,
  usePatchAttributeMutation,
  useUpdateAttributeMutation,
  useListBridgeWorkersQuery,
  useLazyListBridgeWorkersQuery,
  useCreateBridgeWorkerMutation,
  useDeleteBridgeWorkerMutation,
  useGetBridgeWorkerQuery,
  useLazyGetBridgeWorkerQuery,
  usePatchBridgeWorkerMutation,
  useUpdateBridgeWorkerMutation,
  useListBridgeWorkerFunctionsQuery,
  useLazyListBridgeWorkerFunctionsQuery,
  useListBridgeWorkerStatusesQuery,
  useLazyListBridgeWorkerStatusesQuery,
  useGetBridgeWorkerStatusQuery,
  useLazyGetBridgeWorkerStatusQuery,
  useListChangeSetsQuery,
  useLazyListChangeSetsQuery,
  useCreateChangeSetMutation,
  useDeleteChangeSetMutation,
  useGetChangeSetQuery,
  useLazyGetChangeSetQuery,
  usePatchChangeSetMutation,
  useUpdateChangeSetMutation,
  useListFiltersQuery,
  useLazyListFiltersQuery,
  useCreateFilterMutation,
  useDeleteFilterMutation,
  useGetFilterQuery,
  useLazyGetFilterQuery,
  usePatchFilterMutation,
  useUpdateFilterMutation,
  useListFunctionsQuery,
  useLazyListFunctionsQuery,
  useInvokeFunctionsMutation,
  useListInvocationsQuery,
  useLazyListInvocationsQuery,
  useCreateInvocationMutation,
  useDeleteInvocationMutation,
  useGetInvocationQuery,
  useLazyGetInvocationQuery,
  usePatchInvocationMutation,
  useUpdateInvocationMutation,
  useListLinksQuery,
  useLazyListLinksQuery,
  useCreateLinkMutation,
  useDeleteLinkMutation,
  useGetLinkQuery,
  useLazyGetLinkQuery,
  usePatchLinkMutation,
  useUpdateLinkMutation,
  useListTagsQuery,
  useLazyListTagsQuery,
  useCreateTagMutation,
  useDeleteTagMutation,
  useGetTagQuery,
  useLazyGetTagQuery,
  usePatchTagMutation,
  useUpdateTagMutation,
  useListTargetsQuery,
  useLazyListTargetsQuery,
  useCreateTargetMutation,
  useDeleteTargetMutation,
  useGetTargetQuery,
  useLazyGetTargetQuery,
  usePatchTargetMutation,
  useUpdateTargetMutation,
  useListTriggersQuery,
  useLazyListTriggersQuery,
  useCreateTriggerMutation,
  useDeleteTriggerMutation,
  useGetTriggerQuery,
  useLazyGetTriggerQuery,
  usePatchTriggerMutation,
  useUpdateTriggerMutation,
  useListUnitsQuery,
  useLazyListUnitsQuery,
  useCreateUnitMutation,
  useDeleteUnitMutation,
  useGetUnitQuery,
  useLazyGetUnitQuery,
  usePatchUnitMutation,
  useUpdateUnitMutation,
  useApplyUnitMutation,
  useApproveUnitMutation,
  useDownloadUnitDataQuery,
  useLazyDownloadUnitDataQuery,
  useDestroyUnitMutation,
  useGetUnitExtendedQuery,
  useLazyGetUnitExtendedQuery,
  useImportUnitMutation,
  useDownloadUnitLiveDataQuery,
  useLazyDownloadUnitLiveDataQuery,
  useDownloadUnitLiveStateQuery,
  useLazyDownloadUnitLiveStateQuery,
  useListExtendedMutationsQuery,
  useLazyListExtendedMutationsQuery,
  useGetExtendedMutationQuery,
  useLazyGetExtendedMutationQuery,
  useSetUnitPredicatesMutation,
  useRefreshUnitMutation,
  useListExtendedRevisionsQuery,
  useLazyListExtendedRevisionsQuery,
  useGetExtendedRevisionQuery,
  useLazyGetExtendedRevisionQuery,
  useDownloadRevisionDataQuery,
  useLazyDownloadRevisionDataQuery,
  useListUnitActionsQuery,
  useLazyListUnitActionsQuery,
  useGetUnitActionQuery,
  useLazyGetUnitActionQuery,
  useListUnitEventsQuery,
  useLazyListUnitEventsQuery,
  useGetUnitEventQuery,
  useLazyGetUnitEventQuery,
  useListViewsQuery,
  useLazyListViewsQuery,
  useCreateViewMutation,
  useDeleteViewMutation,
  useGetViewQuery,
  useLazyGetViewQuery,
  usePatchViewMutation,
  useUpdateViewMutation,
  useBulkDeleteTagsMutation,
  useListAllTagsQuery,
  useLazyListAllTagsQuery,
  useBulkPatchTagsMutation,
  useBulkCreateTagsMutation,
  useBulkDeleteTargetsMutation,
  useListAllTargetsQuery,
  useLazyListAllTargetsQuery,
  useBulkPatchTargetsMutation,
  useBulkDeleteTriggersMutation,
  useListAllTriggersQuery,
  useLazyListAllTriggersQuery,
  useBulkPatchTriggersMutation,
  useBulkCreateTriggersMutation,
  useBulkDeleteUnitsMutation,
  useListAllUnitsQuery,
  useLazyListAllUnitsQuery,
  useBulkPatchUnitsMutation,
  useBulkCreateUnitsMutation,
  useBulkApplyUnitsMutation,
  useBulkApproveUnitsMutation,
  useBulkCancelUnitsMutation,
  useBulkDestroyUnitsMutation,
  useBulkRefreshUnitsMutation,
  useBulkTagUnitsMutation,
  useListAllUnitActionsQuery,
  useLazyListAllUnitActionsQuery,
  useListAllUnitEventsQuery,
  useLazyListAllUnitEventsQuery,
  useListUsersQuery,
  useLazyListUsersQuery,
  useGetUserQuery,
  useLazyGetUserQuery,
  useBulkDeleteViewsMutation,
  useListAllViewsQuery,
  useLazyListAllViewsQuery,
  useBulkPatchViewsMutation,
  useBulkCreateViewsMutation,
} = injectedRtkApi;
