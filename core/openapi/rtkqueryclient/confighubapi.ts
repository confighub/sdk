// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT
import type { BaseQueryFn } from '@reduxjs/toolkit/query';
import type { FetchArgs, FetchBaseQueryError } from '@reduxjs/toolkit/query';
import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react';

import { getAccessToken } from '@/auth/sdk';

import { instanceUrl } from '@/auth/config';
import { handleUnauthorized } from '@/auth/session';

// fetchBaseQuery uses URLSearchParams, which already escapes query parameters.
// https://redux-toolkit.js.org/rtk-query/api/fetchBaseQuery
// https://developer.mozilla.org/en-US/docs/Web/API/URLSearchParams
// https://github.com/reduxjs/redux-toolkit/pull/4568
//
// Built on first request rather than at import: the URL depends on the runtime
// config, and a module that only imports these endpoints (a Node-side spec) has
// no window to read it from.
let baseQuery: ReturnType<typeof fetchBaseQuery> | undefined;
const getBaseQuery = () =>
  (baseQuery ??= fetchBaseQuery({
    baseUrl: instanceUrl('/api'),
    // No cookies: the API answers cross-origin with Access-Control-Allow-Origin: *,
    // and a browser refuses that combination for a credentialed request.
    credentials: 'same-origin',
    isJsonContentType: (headers) => {
      const ct = headers.get('Content-Type') ?? '';
      return ct.includes('json');
    },
    // Decide how to read a response from what the server says it is, rather than assuming JSON.
    // The configuration endpoints serve the document itself as application/octet-stream, and the
    // default 'json' handler JSON.parses every body -- which fails on YAML and surfaces as a
    // parsing error with no data, i.e. an empty config editor. 'content-type' routes through
    // isJsonContentType above, so those come back as text and everything else is unchanged.
    responseHandler: 'content-type',
    prepareHeaders: (headers, { endpoint }) => {
      const token = getAccessToken();
      if (token) headers.set('Authorization', `Bearer ${token}`);
      // Set content type for operations that require merge-patch+json
      if (
        endpoint.startsWith('patch') ||
        endpoint.startsWith('bulkPatch') ||
        endpoint.startsWith('bulkCreate') ||
        endpoint.startsWith('patchView')
      ) {
        headers.set('Content-Type', 'application/merge-patch+json');
      }
      return headers;
    },
  }));

const baseQueryWithReauth: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError
> = async (args, api, extraOptions) => {
  const result = await getBaseQuery()(args, api, extraOptions);

  if (result.error && result.error.status === 401) {
    // The token is stale or gone; the auth layer decides how to recover.
    handleUnauthorized();
  }

  if (result.error && result.error.status === 403) {
    // Don't redirect if we're already on an error page (prevents infinite loop)
    if (
      window.location.pathname === '/access-denied' ||
      window.location.pathname === '/pending-approval'
    ) {
      return result;
    }

    // Check if this is a "pending approval" error
    const errorData = result.error.data as { message?: string } | undefined;
    const errorMessage = errorData?.message || '';
    const isPendingApproval = errorMessage.includes('pending approval');

    if (isPendingApproval) {
      // The page's "try again" starts a fresh login, which is what picks up the
      // approval; there is no server session to end first.
      window.location.replace('/pending-approval');
    } else {
      // Regular access denied - just show the access denied page
      window.location.replace('/access-denied');
    }

    // Return a promise that never resolves to prevent further query processing
    return new Promise(() => {});
  }

  return result;
};

// initialize an empty api service that we'll inject endpoints into later as needed
export const confighubApi = createApi({
  baseQuery: baseQueryWithReauth,
  endpoints: () => ({}),
});
