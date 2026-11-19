{
:local retainedName "m7c1d813215"
:local owner "meridian-install:1d81321542a1094bce2b7a0b5078aa40"
:local archive "m7c1d813215.tar"
:local image "d57f32457912c5a5b61e90e36f978a9277e9e004b310cdc4bd585fd10e728b03"
:local veth "m7v1d813215"
:local mount "m7m1d813215"
:local env "m7e1d813215"
:local state "m7s1d813215"
:local rows [/container/print as-value where name=$retainedName]
:if ([:len $rows]!=1) do={ :error "M7RECOVERY expected stopped installation not found uniquely" }
:local id (($rows->0)->".id")
:if ([/container/get $id comment]!=$owner || [/container/get $id image-id]!=$image || [:tostr (($rows->0)->"stopped")]!="true" || [/container/get $id file]!=$archive || [/container/get $id root-dir]!=("/" . $retainedName) || [/container/get $id interface]!=$veth || [:tostr [/container/get $id mountlists]]!=$mount || [:tostr [/container/get $id envlists]]!=$env || [/container/get $id start-on-boot]) do={ :error "M7RECOVERY installation differs from saved result" }
:local mounts [/container/mounts/find where list=$mount]
:if ([:len $mounts]!=1 || [/container/mounts/get $mounts src]!=("/" . $state) || [/container/mounts/get $mounts dst]!="/state") do={ :error "M7RECOVERY state mount changed" }
:local links [/interface/veth/find where name=$veth]
:if ([:len $links]!=1 || [/interface/veth/get $links comment]!=$owner || [:len [/interface/bridge/port/find where interface=$veth]]!=0 || [:len [/ip/address/find where interface=$veth]]!=0 || [:len [/ipv6/address/find where interface=$veth and dynamic=no]]!=0) do={ :error "M7RECOVERY interface changed" }
:if ([:len [/ip/firewall/filter/find where comment~$owner]]>0 || [:len [/ip/firewall/nat/find where comment~$owner]]>0 || [:len [/ip/firewall/mangle/find where comment~$owner]]>0 || [:len [/ipv6/firewall/filter/find where comment~$owner]]>0 || [:len [/ipv6/firewall/raw/find where comment~$owner]]>0 || [:len [/ip/firewall/raw/find where comment~$owner]]>0) do={ :error "M7RECOVERY network rules remain" }
:if ([:len [/container/find where name="m7g1d813215"]]>0 || [:len [/interface/bridge/find where comment=$owner]]>0 || [:len [/system/scheduler/find where comment~$owner]]>0 || [:len [/system/script/find where comment~$owner]]>0) do={ :error "M7RECOVERY owned objects remain" }
:foreach other in=[/container/find] do={
 :if ($other!=$id) do={
  :if ([:tostr [/container/get $other interface]]=$veth || [:typeof [:find ("," . [:tostr [/container/get $other mountlists]] . ",") ("," . $mount . ",")]]="num" || [:typeof [:find ("," . [:tostr [/container/get $other envlists]] . ",") ("," . $env . ",")]]="num" || [:tostr [/container/get $other file]]=$archive || [:tostr [/container/get $other root-dir]]=("/" . $retainedName)) do={ :error "M7RECOVERY installation objects shared" }
 }
}
:foreach m in=[/container/mounts/find] do={
 :if ($m!=$mounts) do={
  :local src [:tostr [/container/mounts/get $m src]]
  :if ($src=("/" . $retainedName) || [:pick $src 0 ([:len $retainedName]+2)]=("/" . $retainedName . "/") || $src=("/" . $state)) do={ :error "M7RECOVERY storage shared" }
 }
}
# Refuse removal when another object or the container service shares storage.
:local norm do={
 :local p [:tostr $1]
 :if ([:pick $p 0 1]="/") do={ :set p [:pick $p 1 [:len $p]] }
 :return ($p . "/")
}
:local checkStorage do={
 :local skip $1
 :local normFn $2
 :local rootPath "m7c1d813215/"
 :local archivePath "m7c1d813215.tar/"
 :foreach row in=[/container/print detail as-value] do={
  :if ([:tostr ($row->".id")]!=[:tostr $skip]) do={
   :local root [$normFn ($row->"root-dir")]
   :local layer [$normFn ($row->"layer-dir")]
   :local file [$normFn ($row->"file")]
   :if ([:pick $root 0 [:len $rootPath]]=$rootPath || [:pick $rootPath 0 [:len $root]]=$root || $file=$archivePath || ([:len [:tostr ($row->"layer-dir")]]>0 && ([:pick $layer 0 [:len $rootPath]]=$rootPath || [:pick $rootPath 0 [:len $layer]]=$layer || $layer=$archivePath))) do={ :error "M7RECOVERY shared container storage" }
  }
 }
 :local config [/container/config/print as-value]
 :if ([:typeof ($config->0)]="array") do={ :if ([:len $config]!=1) do={ :error "M7RECOVERY ambiguous container settings" }; :set config ($config->0) }
 :if ([:typeof $config]!="array") do={ :error "M7RECOVERY unreadable container settings" }
 :foreach field in={"tmpdir";"tmp-dir";"layer-dir"} do={
  :local option [:tostr ($config->$field)]
  :if ([:len $option]>0) do={
   :local path [$normFn $option]
   :if ([:pick $path 0 [:len $rootPath]]=$rootPath || [:pick $rootPath 0 [:len $path]]=$path || $path=$archivePath) do={ :error "M7RECOVERY shared service storage" }
  }
 }
 :foreach row in=[/container/mounts/print as-value] do={
  :local src [$normFn ($row->"src")]
  :if ([:pick $src 0 [:len $rootPath]]=$rootPath || [:pick $rootPath 0 [:len $src]]=$src || $src=$archivePath) do={ :error "M7RECOVERY shared mount storage" }
 }
}
$checkStorage $id $norm
:local archives [/file/find where name=$archive]
:local roots [/file/find where name=$retainedName]
:if ([:len $archives]!=1 || [:len $roots]!=1 || [/file/get $roots type]!="container store" || [/file/get $archives size]!=17520640) do={ :error "M7RECOVERY archive or root differs" }
:local a "meridian-oci-v1:"
:local i 0
:while ($i<17520640) do={
 :local n (17520640-$i)
 :if ($n>32768) do={ :set n 32768 }
 :local b ([/file/read file=$archive offset=$i chunk-size=$n as-value]->"data")
 :if ([:len $b]!=$n) do={ :error "M7RECOVERY archive truncated" }
 :set a [:convert ($a . $b) from=raw transform=sha512 to=hex]
 :set i ($i+$n)
}
:if ($a!="b97fd1b150685785126c75a1c5fdd3c70d157fbe65874bc2cfca79d53bf1f47dfbd19b47b31b15aa486b9ef5d2c46133caa0d2af268310844a3e7ada7952ed85") do={ :error "M7RECOVERY archive checksum mismatch" }
:local rootID [:pick $roots 0]
:local archiveID [:pick $archives 0]
:local mountID [:pick $mounts 0]
:local linkID [:pick $links 0]
:local envIDs [/container/envs/find where list=$env]
/container/remove $id
:for retry from=1 to=30 do={ :if ([:len [/container/find where name=$retainedName]]=0) do={ :break }; :delay 1s }
:if ([:len [/container/find where name=$retainedName]]!=0) do={ :error "M7RECOVERY container removal pending" }
:foreach row in=$envIDs do={ :if ([/container/envs/get $row list]!=$env) do={ :error "M7RECOVERY environment changed" }; /container/envs/remove $row }
:if ([:len [/container/mounts/find where list=$mount]]!=1 || [:tostr [:pick [/container/mounts/find where list=$mount] 0]]!=[:tostr $mountID]) do={ :error "M7RECOVERY mount changed" }
/container/mounts/remove $mountID
:for retry from=1 to=30 do={
 :local current [/interface/veth/find where name=$veth]
 :if ([:len $current]=0) do={ :break }
 :if ([:len $current]!=1 || [:tostr [:pick $current 0]]!=[:tostr $linkID] || [/interface/veth/get $current comment]!=$owner || [:len [/container/find where interface=$veth]]>0 || [:len [/interface/bridge/port/find where interface=$veth]]>0) do={ :error "M7RECOVERY interface changed" }
 :onerror busy in={ /interface/veth/remove $linkID } do={ :if ([:typeof [:find [:tostr $busy] "in use by container"]]!="num") do={ :error $busy } }
 :delay 1s
}
:if ([:len [/interface/veth/find where name=$veth]]>0) do={ :error "M7RECOVERY interface removal pending" }
:foreach other in=[/container/find] do={
 :local otherRoot [:tostr [/container/get $other root-dir]]
 :if ([:tostr [/container/get $other file]]=$archive || $otherRoot=("/" . $retainedName) || [:pick $otherRoot 0 ([:len $retainedName]+2)]=("/" . $retainedName . "/")) do={ :error "M7RECOVERY storage became shared" }
}
:foreach m in=[/container/mounts/find] do={
 :local src [:tostr [/container/mounts/get $m src]]
 :if ($src=("/" . $retainedName) || [:pick $src 0 ([:len $retainedName]+2)]=("/" . $retainedName . "/")) do={ :error "M7RECOVERY root became shared" }
}
$checkStorage "" $norm
:local archiveNow [/file/find where name=$archive]
:if ([:len $archiveNow]>1 || ([:len $archiveNow]=1 && [:tostr [:pick $archiveNow 0]]!=[:tostr $archiveID])) do={ :error "M7RECOVERY archive changed" }
:if ([:len $archiveNow]=1) do={ /file/remove $archiveID }
:local rootNow [/file/find where name=$retainedName]
:if ([:len $rootNow]>1 || ([:len $rootNow]=1 && [:tostr [:pick $rootNow 0]]!=[:tostr $rootID])) do={ :error "M7RECOVERY root changed" }
:if ([:len $rootNow]=1) do={ /file/remove $rootID }
:if ([:len [/file/find where name=$archive]]>0 || [:len [/file/find where name=$retainedName]]>0 || [:len [/container/envs/find where list=$env]]>0) do={ :error "M7RECOVERY cleanup incomplete" }
:put ("M7RECOVERY retired previous installation, preserved state=" . $state)
}
