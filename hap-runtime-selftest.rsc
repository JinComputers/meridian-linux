{
:if ([/system/resource/get architecture-name] != "arm64") do={ :error "M7HAP ARM64 required" }
{
# LAB ONLY: insert before fetch or container creation. Not a fit guarantee.
:local m7Free [/system/resource/get free-hdd-space]
:if ([:typeof $m7Free] != "num" || $m7Free < 0) do={ :error "M7SPACE free space unavailable" }
:if ($m7Free < 17162240) do={ :error "M7SPACE insufficient archive" }
:set m7Free ($m7Free - 17162240)
:if ($m7Free < 32210432) do={ :error "M7SPACE insufficient expanded_layers" }
:set m7Free ($m7Free - 32210432)
:if ($m7Free < 21215232) do={ :error "M7SPACE insufficient duplicate_largest_layer" }
:set m7Free ($m7Free - 21215232)
:if ($m7Free < 16777216) do={ :error "M7SPACE insufficient reserve" }
:set m7Free ($m7Free - 16777216)
:put "M7SPACE preflight=pass-estimate-only"

}
:local m7Name ("m7h" . [:rndstr from="abcdefghijklmnopqrstuvwxyz0123456789" length=8])
:local m7ImagePath ($m7Name . ".tar")
:if ([:len [/interface/find where name=$m7Name]] != 0 || [:len [/container/find where name=$m7Name]] != 0 || [:len [/file/find where name=$m7Name]] != 0 || [:len [/file/find where name=$m7ImagePath]] != 0) do={ :error "M7HAP name collision" }
:put ("M7HAP test=" . $m7Name . " archive=" . $m7ImagePath)
:onerror m7Err in={
/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/0d4e370a47d60173566b571397d7e99b29d2c2ac/meridian-hap-runtime-selftest-arm64.tar" dst-path=$m7ImagePath check-certificate=yes http-max-redirect-count=0 idle-timeout=30s
:local m7ImageOK false
# LAB ONLY. Expects m7ImagePath and m7ImageOK in surrounding scope.
# The caller must authenticate this script itself and prohibit untrusted writes.
:set m7ImageOK false
:local m7ExpectedSize 17162240
:local m7ExpectedRoot "c21752bff1cca66fd333310f563e2aef7419340a927921c817424cdca31c778e87d6d75deb6cc4f764e3ca9c5b3012194863004278cd920ff28a870ede83f2f2"
:local m7ChunkSize 32768
:local m7Files [/file/find where name=$m7ImagePath]
:if ([:len $m7Files] != 1) do={ :error "image missing or ambiguous" }
:if ([/file/get $m7Files size] != $m7ExpectedSize) do={ :error "image length mismatch" }
:local m7Offset 0
:local m7Acc "meridian-oci-v1:"
:while ($m7Offset < $m7ExpectedSize) do={
    :local m7Want ($m7ExpectedSize - $m7Offset)
    :if ($m7Want > $m7ChunkSize) do={ :set m7Want $m7ChunkSize }
    :local m7Data ([/file/read file=$m7ImagePath offset=$m7Offset chunk-size=$m7Want as-value]->"data")
    :if ([:len $m7Data] != $m7Want) do={ :error "image read truncated" }
    :set m7Acc [:convert ($m7Acc . $m7Data) from=raw transform=sha512 to=hex]
    :if ([:len $m7Acc] != 128) do={ :error "digest failed" }
    :set m7Offset ($m7Offset + $m7Want)
}
:if ([/file/get $m7Files size] != $m7ExpectedSize) do={ :error "image changed size" }
:if ($m7Acc != $m7ExpectedRoot) do={ :error "image digest mismatch" }
:set m7ImageOK true

:if (!$m7ImageOK) do={ :error "invalid archive" }
{
# LAB ONLY: insert before fetch or container creation. Not a fit guarantee.
:local m7Free [/system/resource/get free-hdd-space]
:if ([:typeof $m7Free] != "num" || $m7Free < 0) do={ :error "M7SPACE free space unavailable" }
:if ($m7Free < 32210432) do={ :error "M7SPACE insufficient expanded_layers" }
:set m7Free ($m7Free - 32210432)
:if ($m7Free < 21215232) do={ :error "M7SPACE insufficient duplicate_largest_layer" }
:set m7Free ($m7Free - 21215232)
:if ($m7Free < 16777216) do={ :error "M7SPACE insufficient reserve" }
:set m7Free ($m7Free - 16777216)
:put "M7SPACE preflight=pass-estimate-only"

}
/interface/veth/add name=$m7Name
/container/add name=$m7Name file=$m7ImagePath root-dir=$m7Name interface=$m7Name cmd=--isolated-hap-selftest start-on-boot=no logging=yes
:local m7Found false
:local m7Count 0
:while (!$m7Found && $m7Count < 60) do={
    :delay 2s
    :set m7Count ($m7Count + 1)
    :local m7ID [/container/find where name=$m7Name]
    :if ([:len $m7ID] = 1) do={
        :set m7Found ([/container/get $m7ID image-id] = "907231a098faedae42ef04ffdf6a753747aebf016768588df31601cb7ebb6e2c")
    }
}
:if (!$m7Found) do={ :error "import was not confirmed" }
/container/start [/container/find where name=$m7Name]
:set m7Count 0
:local m7Finished false
:local m7Final ("^" . $m7Name . ": M7HAP (PASS|FAIL)")
:while (!$m7Finished && $m7Count < 30) do={
    :delay 2s
    :set m7Count ($m7Count + 1)
    :set m7Finished ([:len [/log/find where message~$m7Final]] > 0)
}
:local m7Prefix ("^" . $m7Name . ": M7HAP ")
/log/print where message~$m7Prefix
:if (!$m7Finished) do={ :put "M7HAP test result unconfirmed" }
/container/print detail where name=$m7Name
} do={ :put ("M7HAP error=" . $m7Err) }
:put ("M7HAP retained=" . $m7Name)
}
