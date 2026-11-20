#!/bin/sh
set -eu
if [ "$WEB_APP" = player ]; then
    whep_url=${WHEP_URL:-}
    if [ -z "$whep_url" ]; then
        stream_path=${STREAM_PATH:-live}
        case "$stream_path" in
            *[!a-zA-Z0-9_/-]*|/*|*/|*//*)
                echo 'STREAM_PATH must be a relative MediaMTX path using letters, digits, underscores, hyphens and slashes' >&2
                exit 1
                ;;
        esac
        whep_url="/gateway/$stream_path/whep"
    fi

    # Require URL-encoded input so it can be written directly into JSON.
    export LC_ALL=C
    case "$whep_url" in
        *[![:graph:]]*|*'"'*|*'\'*|//*)
            echo 'WHEP_URL must be a URL-encoded HTTP(S) URL or a same-origin path' >&2
            exit 1
            ;;
        http://?*|https://?*|/?*)
            printf '{"whep":"%s"}\n' "$whep_url" > /usr/share/nginx/html/runtime.json
            ;;
        *)
            echo 'WHEP_URL must be a URL-encoded HTTP(S) URL or a same-origin path' >&2
            exit 1
            ;;
    esac
fi
