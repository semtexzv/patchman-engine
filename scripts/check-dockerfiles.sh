#!/bin/bash

rc=0

# Check consistent testing (centos) and production (redhat) dockerfiles.
for dockerfile in database/Dockerfile Dockerfile
do
    if [ ! -f "$dockerfile" ]; then
        echo "Dockerfile '$dockerfile' doesn't exist" >&2
        rc=$(($rc+1))
    fi
    if [ -f "$dockerfile.rhel7" ]; then
      # Rhel 7 dockerfile exists, compare it with centos dockerfile
      sed \
          -e "s/centos:7/registry.access.redhat.com\/rhel7/" \
          -e "s/centos\/postgresql-10-centos7/registry.access.redhat.com\/rhscl\/postgresql-10-rhel7/" \
          -e "s/yum -y install centos-release-scl/yum-config-manager --enable rhel-server-rhscl-7-rpms/" \
          "$dockerfile" | diff "${dockerfile}.rhel7" -
      diff_rc=$?
      if [ $diff_rc -gt 0 ]; then
          echo "$dockerfile and $dockerfile.rhel7 are too different!"
      else
        echo "$dockerfile and $dockerfile.rhel7 are OK"
      fi
      rc=$(($rc+$diff_rc))
      continue
    fi

    if [ -f "$dockerfile.rhel8" ]; then
      # Rhel 8 dockerfile exists, compare it with centos dockerfile
      sed \
          -e "s/centos:8/registry.access.redhat.com\/ubi8/" \
          "$dockerfile" | diff "${dockerfile}.rhel8" -
      diff_rc=$?
      if [ $diff_rc -gt 0 ]; then
        echo "$dockerfile and $dockerfile.rhel8 are too different!"
      else
        echo "$dockerfile and $dockerfile.rhel8 are OK"
      fi
      rc=$(($rc+$diff_rc))
      continue
    fi
    echo "$dockerfile has no RHEL alternative"
    exit 1
done
echo ""

exit $rc