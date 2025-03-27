#!/usr/bin/env bash
set -euo pipefail

# if first argument is "test", use gotestsum
if [ "${1:-}" == "test" ]; then
	shift

	cc=0
	ff=0
	real_args=()
	extra_args=""
	max_lines=1000 # Default to 1000 lines

	# Handle each argument
	for arg in "$@"; do
		if [ "$arg" = "-function-coverage" ]; then
			cc=1
		elif [ "$arg" = "-force" ]; then
			ff=1
		elif [[ "$arg" =~ ^-max-lines=(.*)$ ]]; then
			max_lines="${BASH_REMATCH[1]}"
		else
			real_args+=("$arg")
		fi
	done

	if [[ "$cc" == "1" ]]; then
		tmpcoverdir=$(mktemp -d)
		function print_coverage() {
			echo "================================================"
			echo "Function Coverage"
			echo "------------------------------------------------"
			go tool cover -func=$tmpcoverdir/coverage.out
			echo "================================================"

		}
		extra_args=" -coverprofile=$tmpcoverdir/coverage.out -covermode=atomic "
		trap "print_coverage" EXIT
	fi

	if [[ "$ff" == "1" ]]; then
		extra_args="$extra_args -count=1 "
	fi

	# Use our truncation wrapper - go run ./cmd/test-deco
	./scripts/truncate-test-logs.sh "$max_lines" -- go tool gotest.tools/gotestsum \
		--format pkgname \
		--format-icons hivis \
		--hide-summary=skipped \
		\
		--raw-command -- go test -v -vet=all -json -cover $extra_args "${real_args[@]}" # --jsonfile=test.json \

	exit $?
fi

if [ "${1:-}" == "tool" ]; then
	shift
	escape_regex() {
		printf '%s\n' "$1" | sed 's/[][(){}.*+?^$|\\]/\\&/g'
	}
	errors_to_suppress=(
		# https://github.com/protocolbuffers/protobuf-javascript/issues/148
		"plugin.proto#L122"
	)
	# 🔧 Build regex for suppressing errors
	errors_to_suppress_regex=""
	for phrase in "${errors_to_suppress[@]}"; do
		escaped_phrase=$(escape_regex "$phrase")
		if [[ -n "$errors_to_suppress_regex" ]]; then
			errors_to_suppress_regex+="|"
		fi
		errors_to_suppress_regex+="$escaped_phrase"
	done

	# 'go tool -n "$@"' can but used to get the binary name that is being run in case we need it later
	tool_binary_executable=$(go tool -n "$@")

	go tool "$@" <&0 >&1 2> >(sed -E '\^'"$errors_to_suppress_regex"'^d' >&2)

	exit $?
fi

# otherwise run go directly with all arguments
go "$@"
