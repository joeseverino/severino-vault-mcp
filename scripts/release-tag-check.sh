#!/bin/sh
# Usage: release-tag-check.sh <tag> <module path>
#
# Refuses a release tag that is not vMAJOR.MINOR.PATCH, or whose major the
# module path does not carry: `go install ...@vN.x.y` resolves only when the
# path ends in /vN for a major of 2 or more, and in no /vN for v0 and v1.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: release-tag-check.sh <tag> <module path>" >&2
  exit 2
fi
tag=$1
module=$2

# Each of MAJOR, MINOR and PATCH is digits with no leading zero, as SemVer
# requires; anything else in the tag (a pre-release, a newline) is refused.
fail_semver() {
  echo "::error::tag $tag is not vMAJOR.MINOR.PATCH"
  exit 1
}
number() {
  case $1 in
    '' | *[!0-9]* | 0?*) fail_semver ;;
  esac
}
case $tag in
  v*) ;;
  *) fail_semver ;;
esac
rest=${tag#v}
major=${rest%%.*}
rest=${rest#"$major"}
rest=${rest#.}
minor=${rest%%.*}
rest=${rest#"$minor"}
rest=${rest#.}
patch=$rest
number "$major"
number "$minor"
number "$patch"
[ "$tag" = "v$major.$minor.$patch" ] || fail_semver

case $major in
  0 | 1) want="" ;;
  *) want="/v$major" ;;
esac

# A major version suffix is a last path element of v and digits only.
last=${module##*/}
have=""
if [ "$last" != "$module" ]; then
  case $last in
    v[0-9]*)
      case ${last#v} in
        *[!0-9]*) ;;
        *) have="/$last" ;;
      esac
      ;;
  esac
fi

if [ "$have" != "$want" ]; then
  if [ -n "$want" ]; then
    echo "::error file=go.mod::tag $tag needs the module path to end in '$want'; go.mod says $module"
  else
    echo "::error file=go.mod::tag $tag needs a module path with no major version suffix; go.mod says $module"
  fi
  exit 1
fi
