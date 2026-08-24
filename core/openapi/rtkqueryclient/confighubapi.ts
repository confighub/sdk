// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT
import type { BaseQueryFn } from '@reduxjs/toolkit/query';
import type { FetchArgs, FetchBaseQueryError } from '@reduxjs/toolkit/query';
import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react';

// fetchBaseQuery uses URLSearchParams, which already escapes query parameters.
// https://redux-toolkit.js.org/rtk-query/api/fetchBaseQuery
// https://developer.mozilla.org/en-US/docs/Web/API/URLSearchParams
// https://github.com/reduxjs/redux-toolkit/pull/4568
const baseQuery = fetchBaseQuery({
  baseUrl: '/api',
  credentials: 'include', // This enables sending cookies with requests
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
});

const baseQueryWithReauth: BaseQueryFn<
  string | FetchArgs,
  unknown,
  FetchBaseQueryError
> = async (args, api, extraOptions) => {
  let result = await baseQuery(args, api, extraOptions);

  if (result.error && result.error.status === 401) {
    // Unauthorized - redirect to login
    const redirectUri = encodeURIComponent(`${window.location.origin}/auth/callback`);
    window.location.href =
      '/auth/login?state=' +
      window.location.pathname +
      (window.location.search ?? '') +
      '&redirect_uri=' +
      redirectUri;
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
      // For pending approval, log out the user first so they get a fresh JWT when approved
      // The return_to parameter will bring them back to the pending approval page
      const returnTo = encodeURIComponent(`${window.location.origin}/pending-approval`);
      window.location.href = `/auth/logout?return_to=${returnTo}`;
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
