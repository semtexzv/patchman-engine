#!/usr/bin/env bash

if [ -z "USE_RDS" ]; then
  # Connect to RDS
  # Migrate
  # Change user passwords if necessary
else
  # uses default container machinery to init DB
  # run migrations
  run-postgresql
fi
