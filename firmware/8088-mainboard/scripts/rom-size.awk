# NASM listing offsets identify both padding regions; actual FF bytes inside
# code/data remain counted. Use portable hex conversion (no GNU strtonum).
function from_hex(value, result, i) {
    result = 0
    value = tolower(value)
    if (value !~ /^[0-9a-f]+$/) {
        parse_error = 1
        return 0
    }
    for (i = 1; i <= length(value); i++) {
        result = result * 16 + index("0123456789abcdef", substr(value, i, 1)) - 1
    }
    return result
}

/times 7FF0h -/ {
    # An empty padding directive has no address/bytes field in the listing.
    body_size = from_hex($2 == "times" ? "7ff0" : $2)
    body_found = 1
}
/times 8000h -/ { vector_end = from_hex($2); vector_found = 1 }

END {
    vector_start = from_hex("7ff0")
    if (parse_error || !body_found || !vector_found || body_size > vector_start ||
        vector_end < vector_start || vector_end > image_size || image_size <= 0) {
        print "Cannot determine BIOS ROM usage from NASM listing" > "/dev/stderr"
        exit 1
    }
    used = body_size + vector_end - vector_start
    printf "BIOS ROM: %d / %d bytes (%.2f%% used), %d bytes free\n", \
        used, image_size, used * 100 / image_size, image_size - used
}
