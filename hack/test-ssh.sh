#!/bin/bash
set -e

echo "Trying to connect to VM via SSH..."
ssh -p 10022 ubuntu@localhost -o StrictHostKeyChecking=no "echo 'Connection successful!' && uname -a"
