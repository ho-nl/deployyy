# The programs image: recipes/<recipe>/platform/bin/ of this commit. The
# operator mounts the folder of a recipe version at /platform/bin/ in the
# processes and steps that run a program.
FROM scratch
COPY recipes/ /recipes/
