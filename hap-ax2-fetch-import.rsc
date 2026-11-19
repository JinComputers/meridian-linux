{
:local m7ImageOK false
:if ([/system/resource/get architecture-name] != "arm64") do={ :error "M7IMPORT ARM64 required" }
{
:local m7Free [/system/resource/get free-hdd-space]
:if ([:typeof $m7Free] != "num" || $m7Free < 0) do={ :error "M7SPACE free space unavailable" }
:if ($m7Free < 17141760) do={ :error "M7SPACE insufficient archive" }
:set m7Free ($m7Free - 17141760)
:if ($m7Free < 32187904) do={ :error "M7SPACE insufficient expanded_layers" }
:set m7Free ($m7Free - 32187904)
:if ($m7Free < 21215232) do={ :error "M7SPACE insufficient duplicate_largest_layer" }
:set m7Free ($m7Free - 21215232)
:if ($m7Free < 16777216) do={ :error "M7SPACE insufficient reserve" }
:set m7Free ($m7Free - 16777216)
:put "M7SPACE preflight=pass-estimate-only"
}
:local m7Name ("m7i" . [:rndstr from="abcdefghijklmnopqrstuvwxyz0123456789" length=8])
:local m7ImagePath ($m7Name . ".tar")
:if ([:len [/interface/find where name=$m7Name]] != 0 || [:len [/container/find where name=$m7Name]] != 0 || [:len [/file/find where name=$m7Name]] != 0 || [:len [/file/find where name=$m7ImagePath]] != 0) do={ :error "M7IMPORT name collision" }
:put ("M7IMPORT name=" . $m7Name . " archive=" . $m7ImagePath)
:onerror m7Error in={
/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/3f13e32c4584b40dd6a0ab010609840a5514e445/meridian-upx-oci-arm64.tar" dst-path=$m7ImagePath check-certificate=yes http-max-redirect-count=0 idle-timeout=30s
:set m7ImageOK false
:local m7ExpectedSize 17141760
:local m7ExpectedRoot "f6cc1e5528eecb7d4cdea1d9428c9e1528c0a97bc5505f5751fe8e9381372d2a8d910d25fd1314ab1973f3a688d12ee5a456a6153614cf3cab80dfa6140619cd"
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
:if (!$m7ImageOK) do={ :error "M7IMPORT verification failed" }
:local m7Free [/system/resource/get free-hdd-space]
:if ([:typeof $m7Free] != "num" || $m7Free < 0) do={ :error "M7SPACE free space unavailable" }
:if ($m7Free < 32187904) do={ :error "M7SPACE insufficient expanded_layers" }
:set m7Free ($m7Free - 32187904)
:if ($m7Free < 21215232) do={ :error "M7SPACE insufficient duplicate_largest_layer" }
:set m7Free ($m7Free - 21215232)
:if ($m7Free < 16777216) do={ :error "M7SPACE insufficient reserve" }
:set m7Free ($m7Free - 16777216)
:put "M7SPACE preflight=pass-estimate-only"
/interface/veth/add name=$m7Name
/container/add name=$m7Name file=$m7ImagePath root-dir=$m7Name interface=$m7Name logging=no start-on-boot=no
:local m7Attempt 0
:local m7Done false
:while (!$m7Done && $m7Attempt < 30) do={
    :set m7Attempt ($m7Attempt + 1)
    :delay 2s
    :local m7Rows [/container/print as-value where name=$m7Name]
    :if ([:len $m7Rows] = 1) do={
        :local m7Row ($m7Rows->0)
        :if ([:tostr ($m7Row->"stopped")] = "true" && ($m7Row->"arch") = "arm64" && [:len ($m7Row->"image-id")] > 0) do={
            :put ("M7IMPORT imported=yes started=no free-after=" . [/system/resource/get free-hdd-space])
            :set m7Done true
        }
    }
}
:if (!$m7Done) do={ :put "M7IMPORT import unconfirmed" }
/container/print detail where name=$m7Name
:put ("M7IMPORT retained=" . $m7Name)
} do={ :put ("M7IMPORT error=" . $m7Error . " retained=" . $m7Name) }
}
