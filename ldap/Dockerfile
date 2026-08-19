FROM osixia/openldap:1.5.0

# The base image chowns/renames/removes files inside the bootstrap ldif
# directory as part of its own setup process, which makes a host bind mount
# unsafe (it will delete the mounted host file). Baking the ldif into the
# image instead keeps the source file in the repository untouched.
COPY config.ldif /container/service/slapd/assets/config/bootstrap/ldif/custom/config.ldif
