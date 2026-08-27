# The API version -- the first two numbers of every ConfigHub version -- is declared
# here, once, because it describes the product's API rather than any one binary. Every
# Makefile that stamps a version into a binary includes this file, so that a single build
# cannot end up stamping two different answers.
#
# It lives here, alongside the code published to the SDK repository, because cub is built
# from that repository as well as from this one, and a declaration those builds cannot see
# is a declaration they would not be held to. The paths that include it are relative to
# this file's directory, which is the SDK repository's root, so they resolve either way.
#
# Pre-1.0, the second number is what changes when an API change is not backward
# compatible, and the third number carries everything that is. cub compares its first two
# numbers against the server's and refuses to run against a server it is behind, so what a
# build stamps has to be deliberate rather than whatever the tag happened to say.
#
# Bump API_MINOR in the same change that breaks the API. The next release tag then has to
# start with the new v$(API_MAJOR).$(API_MINOR), and a tag that does not fails the build
# below rather than shipping a binary whose version misdescribes the API it speaks.

API_MAJOR := 0
API_MINOR := 4

ifeq ($(strip $(VERSION)),)
  # Nobody named a version for this build, so it is a development build. It still says
  # which API it speaks: a dev cub and a dev server built from the same tree agree, and a
  # dev cub against a released server of another API version is caught the same way a
  # released cub would be. The "-dev" suffix is not part of the comparison; it marks a
  # build that came from a working tree rather than a tag.
  override VERSION := v$(API_MAJOR).$(API_MINOR)-dev
else ifeq ($(filter v$(API_MAJOR).$(API_MINOR).%,$(VERSION)),)
  $(error VERSION $(VERSION) does not match the API version v$(API_MAJOR).$(API_MINOR) declared in $(lastword $(MAKEFILE_LIST)). Tag a v$(API_MAJOR).$(API_MINOR).x release, or -- if this release changes the API incompatibly -- bump API_MINOR in $(lastword $(MAKEFILE_LIST)) first)
endif
