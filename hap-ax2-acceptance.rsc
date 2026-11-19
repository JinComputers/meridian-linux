{
:if ([/system/resource/get architecture-name] != "arm64" || ([/system/resource/get version] != "7.25.1" && [:pick [/system/resource/get version] 0 7] != "7.25.1 ")) do={ :error "Supported target: ARM64 RouterOS 7.25.1" }
:if ([:len [/system/package/find where name="container" and disabled=no]] != 1 || ![/system/device-mode/get container]) do={ :error "Container package and device-mode required" }
:local acNonce [:rndstr from="0123456789abcdef" length=12]
:local m7InstallLan ("acb" . $acNonce)
:local acVeth ("acv" . $acNonce)
:local acContainer ("acc" . $acNonce)
:local acMount ("acm" . $acNonce)
:local acEnv ("ace" . $acNonce)
:local acOwner ("meridian-accept:" . $acNonce)
:local acAuthReceipt
:local acSource
:local routerIP
:local clientNetwork
:foreach n in={208;209;210;211;212;213;214;215} do={
:local begin [:toip ("172.31." . $n . ".0")]
:local end [:toip ("172.31." . $n . ".3")]
:local conflict false
:foreach rid in=[/ip/route/find] do={
:local scope [/ip/route/get $rid dst-address]
:if ($scope!=0.0.0.0/0) do={
:local text [:tostr $scope]
:local first [:toip [:pick $text 0 [:find $text "/"]]]
:if (($begin in $scope) || ($end in $scope) || ($first>=$begin && $first<=$end)) do={ :set conflict true }
}
}
:foreach ident in=[/ip/address/find] do={
:local prefix [/ip/address/get $ident address]
:local first [/ip/address/get $ident network]
:if (($begin in $prefix) || ($end in $prefix) || ($first>=$begin && $first<=$end)) do={ :set conflict true }
}
:if (!$conflict && [:typeof $routerIP]="nothing") do={
:set clientNetwork ([:tostr $begin] . "/30")
:set routerIP [:toip ("172.31." . $n . ".1")]
:set acSource [:toip ("172.31." . $n . ".2")]
}
}
:if ([:typeof $routerIP]!="ip") do={ :error "M7ACCEPT no unused dedicated subnet" }
:if ([:len [/interface/find where name=$m7InstallLan or name=$acVeth]]>0 || [:len [/container/find where name=$acContainer]]>0 || [:len [/container/envs/find where list=$acEnv]]>0 || [:len [/container/mounts/find where list=$acMount]]>0) do={ :error "M7ACCEPT probe object collision" }
:local m7InstallRoute [/ip/route/find where dst-address=0.0.0.0/0 and active=yes and routing-table=main]
:if ([:len $m7InstallRoute] != 1) do={ :error "WAN default route must be unique" }
:local gatewayText [:tostr [/ip/route/get $m7InstallRoute immediate-gw]]
:local perc [:find $gatewayText "%"]
:local m7InstallWan $gatewayText
:if ([:typeof $perc] = "num") do={ :set m7InstallWan [:pick $gatewayText ($perc + 1) [:len $gatewayText]] }
:if ($m7InstallWan = $m7InstallLan || [:len [/interface/find where name=$m7InstallWan and running=yes]] != 1) do={ :error "WAN path is ambiguous" }
:local m7InstallPort 18443
:if ([:len [/ip/service/find where disabled=no and port=$m7InstallPort]] > 0) do={ :error "Panel port in use" }
:foreach rule in=[/ip/firewall/nat/find where chain=dstnat and disabled=no] do={
:local configuredPort [:tostr [/ip/firewall/nat/get $rule dst-port]]
:local entryInterface [:tostr [/ip/firewall/nat/get $rule in-interface]]
:local ruleProto [:tostr [/ip/firewall/nat/get $rule protocol]]
:if (($entryInterface = "" || $entryInterface = $m7InstallLan || [:pick $entryInterface 0 1] = "!") && ($ruleProto = "" || $ruleProto = "tcp" || $ruleProto = "6")) do={
:local portConflict ($configuredPort = "")
:foreach entry in=[:toarray $configuredPort] do={
:local segment [:tostr $entry]
:local dash [:find $segment "-"]
:local low [:tonum $segment]
:local high $low
:if ([:typeof $dash] = "num") do={
:set low [:tonum [:pick $segment 0 $dash]]
:set high [:tonum [:pick $segment ($dash + 1) [:len $segment]]]
}
:if ([:typeof $low] != "num" || [:typeof $high] != "num") do={ :set portConflict true } else={
:if ($m7InstallPort >= $low && $m7InstallPort <= $high) do={ :set portConflict true }
}
}
:if ($portConflict) do={ :error "Existing destination NAT might use the panel port" }
}
}
:if ([:len [/ip/firewall/filter/find where action=fasttrack-connection and disabled=no]] != 0) do={ :error "Active FastTrack requires routing review" }
:local m7InstallGateway
:local m7InstallBackend
:foreach n in={0;4;8;12;16;20;24;28} do={
:local prefix [:toip ("198.18.0." . $n)]
:local last [:toip ("198.18.0." . ($n + 3))]
:local collision false
:foreach r in=[/ip/route/find] do={
:local network [/ip/route/get $r dst-address]
:if ($network != 0.0.0.0/0 && (($prefix in $network) || ($last in $network))) do={ :set collision true }
:local existing [:tostr $network]
:local baseIP [:toip [:pick $existing 0 [:find $existing "/"]]]
:if ($network != 0.0.0.0/0 && $baseIP >= $prefix && $baseIP <= $last) do={ :set collision true }
}
:foreach a in=[/ip/address/find] do={
:local assigned [/ip/address/get $a address]
:local first [/ip/address/get $a network]
:if (($prefix in $assigned) || ($last in $assigned) || ($first >= $prefix && $first <= $last)) do={ :set collision true }
}
:if (!$collision && [:typeof $m7InstallGateway] = "nothing") do={
:set m7InstallGateway [:toip ("198.18.0." . ($n + 1))]
:set m7InstallBackend [:toip ("198.18.0." . ($n + 2))]
}
}
:if ([:typeof $m7InstallGateway] != "ip") do={ :error "No unused container /30 available" }
:local m7InstallTag [:rndstr from="0123456789abcdef" length=32]
:local m7InstallOwner ("meridian-install:" . $m7InstallTag)
:local suffix [:pick $m7InstallTag 0 8]
:local m7InstallName ("m7c" . $suffix)
:local gateName ("m7g" . $suffix)
:local m7InstallBridge ("m7b" . $suffix)
:local m7InstallVeth ("m7v" . $suffix)
:local m7InstallEnv ("m7e" . $suffix)
:local gateEnv ("m7q" . $suffix)
:local m7InstallMount ("m7m" . $suffix)
:local flash ""
:if ([:len [/file/find where name="flash" and type="disk"]] = 1) do={ :set flash "flash/" }
:local rootDir ($flash . $m7InstallName)
:local gateRoot ($flash . $gateName)
:local gateArchive ($flash . $gateName . ".tar")
:local stateDir ($flash . "m7s" . $suffix)
:local m7ImagePath ($flash . $m7InstallName . ".tar")
:if ([:len [/container/find where name=$m7InstallName or name=$gateName]] > 0 || [:len [/interface/find where name=$m7InstallVeth or name=$m7InstallBridge]] > 0 || [:len [/container/envs/find where list=$m7InstallEnv or list=$gateEnv]] > 0 || [:len [/container/mounts/find where list=$m7InstallMount]] > 0) do={ :error "Installation name collision" }
:if ([:len [/container/find where comment~"^meridian-install:"]] > 0) do={ :error "Existing Meridian installation found; inspect it before retry" }
:foreach item in=($rootDir,$stateDir,$m7ImagePath,$gateRoot,$gateArchive) do={ :if ([:len [/file/find where name=$item]] > 0) do={ :error "Installation path collision" } }
:local m7FetchImage do={
:local m7ImagePath $1
:local m7Url $2
:local m7ExpectedSize $3
:local m7ExpectedRoot $4
:local m7SpaceAfter $5
:if ([/system/resource/get free-hdd-space] < ($m7ExpectedSize + $m7SpaceAfter)) do={ :error "Insufficient disk space for this image" }
/tool/fetch url=$m7Url dst-path=$m7ImagePath check-certificate=yes http-max-redirect-count=0 idle-timeout=30s
:local m7ImageOK false
:set m7ImageOK false
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
:if (!$m7ImageOK || [/system/resource/get free-hdd-space] < $m7SpaceAfter) do={ :error "Archive verification or extraction space failed" }
:return true
}
:local m7WaitImport do={
:local candidateID $1
:local expectedID $2
:for i from=1 to=120 do={
:if ([/container/get $candidateID image-id] = $expectedID) do={ :return true }
:delay 2s
}
:error "Import did not match verified image"
}
:local requiredBytes 88081920
:local availableBytes [/system/resource/get free-hdd-space]
:if ([:typeof $availableBytes]="num" && $availableBytes < $requiredBytes) do={
:if ([:len [/container/find where name="m7irjwqw65w"]]!=1) do={ :error "M7ACCEPT insufficient flash; known old diagnostic container absent" }
:put "M7ACCEPT verifying previously authorized old diagnostic before freeing space"
{
:local acName "m7irjwqw65w"
:local acArchive "m7irjwqw65w.tar"
:local acImage "af9bf5f673ff7cde9117093dd2bef38a1b78718d4a2201f9ce1787d9dc15954a"
:local acRootID
:local acArchiveID
:local acContainerID
:local acRows [/container/print detail as-value where name=$acName]
:local acRoots [/file/find where name=$acName]
:local acArchives [/file/find where name=$acArchive]
:if ([:len $acRows]>1 || [:len $acRoots]>1 || [:len $acArchives]>1) do={ :error "M7OLD ambiguous objects" }
:if ([:len $acRows]=1) do={
:local row ($acRows->0)
:if (($row->"image-id")!=$acImage || ($row->"arch")!="arm64" || ($row->"os")!="linux" || ($row->"tag")!="lab" || ($row->"file")!=$acArchive || ($row->"root-dir")!=("/" . $acName) || ($row->"interface")!=$acName || [:len [:tostr [/container/get ($row->".id") mountlists]]]!=0 || [:len [:tostr [/container/get ($row->".id") envlists]]]!=0 || [:tostr ($row->"stopped")]!="true" || [:tostr ($row->"start-on-boot")]="true") do={ :error "M7OLD container identity or stopped state differs" }
:set acContainerID ($row->".id")
:if ([:len $acRoots]=1) do={ :if ([/file/get $acRoots type]!="directory") do={ :error "M7OLD root is not directory" }; :set acRootID [:pick $acRoots 0] }
}
:local acNorm do={ :local p [:tostr $1]; :if ([:pick $p 0 1]="/") do={ :set p [:pick $p 1 [:len $p]] }; :return ($p . "/") }
:local acFree do={
:local root ($1 . "/")
:local archive $2
:local skip $3
:local norm $4
:foreach r in=[/container/print detail as-value] do={
:if ([:tostr ($r->".id")]!=[:tostr $skip]) do={
:local p [$norm ($r->"root-dir")]
:if ([:pick $p 0 [:len $root]]=$root || [:pick $root 0 [:len $p]]=$p || ($r->"file")=$archive) do={ :error "M7OLD shared container storage" }
:local layer [:tostr ($r->"layer-dir")]
:if ([:len $layer]>0) do={ :local l [$norm $layer]; :if ([:pick $l 0 [:len $root]]=$root || [:pick $root 0 [:len $l]]=$l || $l=($archive . "/")) do={ :error "M7OLD shared layer storage" } }
}
}
:local conf [/container/config/print as-value]
:if ([:typeof ($conf->0)]="array") do={ :if ([:len $conf]!=1) do={ :error "M7OLD ambiguous container storage configuration" }; :set conf ($conf->0) }
:if ([:typeof $conf]!="array") do={ :error "M7OLD unreadable container storage configuration" }
:foreach field in={"tmpdir";"tmp-dir";"layer-dir"} do={
:local option [:tostr ($conf->$field)]
:if ([:len $option]>0) do={ :local d [$norm $option]; :if ([:pick $d 0 [:len $root]]=$root || [:pick $root 0 [:len $d]]=$d || $d=($archive . "/")) do={ :error "M7OLD configured shared storage" } }
}
:foreach r in=[/container/mounts/print as-value] do={
:local p [$norm ($r->"src")]
:if ([:pick $p 0 [:len $root]]=$root || [:pick $root 0 [:len $p]]=$p || $p=($archive . "/")) do={ :error "M7OLD shared mount storage" }
}
}
$acFree $acName $acArchive $acContainerID $acNorm
:if ([:len $acArchives]=1) do={
:set acArchiveID [:pick $acArchives 0]
:if ([/file/get $acArchiveID size]!=17141760) do={ :error "M7OLD old archive size mismatch" }
:local pos 0
:local acc "meridian-oci-v1:"
:while ($pos<17141760) do={
:local n (17141760 - $pos)
:if ($n>32768) do={ :set n 32768 }
:local b ([/file/read file=$acArchive offset=$pos chunk-size=$n as-value]->"data")
:if ([:len $b]!=$n) do={ :error "M7OLD archive truncated" }
:set acc [:convert ($acc . $b) from=raw transform=sha512 to=hex]
:set pos ($pos+$n)
}
:if ($acc!="f6cc1e5528eecb7d4cdea1d9428c9e1528c0a97bc5505f5751fe8e9381372d2a8d910d25fd1314ab1973f3a688d12ee5a456a6153614cf3cab80dfa6140619cd") do={ :error "M7OLD archive checksum differs" }
}
:if ([:len [:tostr $acContainerID]]>0) do={
:local rows [/container/print detail as-value where name=$acName]
:if ([:len $rows]!=1 || [:tostr (($rows->0)->".id")]!=[:tostr $acContainerID] || (($rows->0)->"image-id")!=$acImage || [:tostr (($rows->0)->"stopped")]!="true") do={ :error "M7OLD container changed" }
/container/remove [:toid $acContainerID]
}
$acFree $acName $acArchive "" $acNorm
:if ([:len [:tostr $acArchiveID]]>0 && [:tostr [:pick [/file/find where name=$acArchive] 0]]=[:tostr $acArchiveID]) do={ /file/remove $acArchiveID }
:if ([:len [:tostr $acRootID]]>0 && [:tostr [:pick [/file/find where name=$acName] 0]]=[:tostr $acRootID]) do={ /file/remove $acRootID }
:put ("M7OLD remaining-root=" . [:len [/file/find where name=$acName]] . " free-bytes=" . [/system/resource/get free-hdd-space])
}
:set availableBytes [/system/resource/get free-hdd-space]
}
:if ([:typeof $availableBytes] != "num" || $availableBytes < $requiredBytes) do={ :error ("Insufficient disk space: required " . $requiredBytes . " bytes, available " . $availableBytes) }
:local stablePath ($flash . "meridian-device-id.txt")
:local stableFile [/file/find where name=$stablePath]
:if ([:len $stableFile] = 0) do={
:local newDevice ("router-" . [:rndstr from="0123456789abcdef" length=32])
:onerror alreadyCreated in={ /file/add name=$stablePath type=file contents=$newDevice } do={}
:set stableFile [/file/find where name=$stablePath]
}
:if ([:len $stableFile] != 1 || ([/file/get $stableFile type] != ".txt file" && [/file/get $stableFile type] != "file") || [/file/get $stableFile size] != 39) do={ :error "Persistent Meridian router identity unavailable" }
:local deviceID [/file/get $stableFile contents]
:if ([:len $deviceID] != 39 || !($deviceID ~ "^router-[0-9a-f]+\$")) do={ :error "Invalid persistent Meridian router identity" }
:local m7GateState [:toarray ""]
:local m7RetireGate do={
:local gc $1
:local gateID ($gc->"gateID")
:local gateName ($gc->"gateName")
:local m7InstallOwner ($gc->"owner")
:local m7InstallMount ($gc->"mount")
:local gateEnv ($gc->"gateEnv")
:local m7InstallVeth ($gc->"veth")
:local gateImageID ($gc->"gateImageID")
:local gateEnvID ($gc->"gateEnvID")
:local gateArchive ($gc->"gateArchive")
:local originalRootID ($gc->"originalRootID")
:local gateRoot ($gc->"gateRoot")
:local deviceID ($gc->"deviceID")
:if ([/container/get $gateID image-id] != $gateImageID || [/container/get $gateID name] != $gateName || [/container/get $gateID comment] != $m7InstallOwner || [:tostr [/container/get $gateID mountlists]] != $m7InstallMount || [:tostr [/container/get $gateID envlists]] != $gateEnv || [/container/get $gateID interface] != $m7InstallVeth) do={ :error "Verifier ownership changed" }
:onerror alreadyStopped in={ /container/stop $gateID } do={}
:for i from=1 to=40 do={
:local existing [/container/find where name=$gateName and comment=$m7InstallOwner]
:if ([:len $existing] = 0) do={ :break }
:if ([:len $existing] != 1 || [:tostr [:pick $existing 0]] != [:tostr $gateID] || [/container/get $gateID image-id] != $gateImageID) do={ :error "Verifier ownership changed" }
:onerror busy in={ /container/remove $gateID } do={}
:delay 1s
}
:if ([:len [/container/find where name=$gateName]] > 0) do={ :error "Verifier shutdown incomplete" }
:local currentGateEnv [/container/envs/find where list=$gateEnv and key=M7_DEVICE_ID and value=$deviceID]
:if ([:len $currentGateEnv] != 1 || [:tostr [:pick $currentGateEnv 0]] != [:tostr $gateEnvID]) do={ :error "Verifier environment changed" }
/container/envs/remove $gateEnvID
:local oldArchive [/file/find where name=$gateArchive]
:if ([:len $oldArchive] != 1 || [:tostr [:pick $oldArchive 0]] != [:tostr ($gc->"archiveID")] || [/file/get $oldArchive size] != ($gc->"size")) do={ :error "Verifier archive ownership changed" }
/file/remove $oldArchive
:local remainingRoot [/file/find where name=$gateRoot]
:if ([:len $remainingRoot] > 0) do={
:if ([:len $remainingRoot] != 1 || [:tostr [:pick $remainingRoot 0]] != [:tostr $originalRootID] || [/file/get $remainingRoot type] != "directory") do={ :error "Verifier root changed" }
:foreach otherContainer in=[/container/find] do={
:local otherPath ([:tostr [/container/get $otherContainer root-dir]] . "/")
:if ([:pick $otherPath 0 ([:len $gateRoot] + 2)] = ("/" . $gateRoot . "/")) do={ :error "Verifier root in use" }
}
:foreach otherMount in=[/container/mounts/find] do={
:local otherPath ([:tostr [/container/mounts/get $otherMount src]] . "/")
:if ([:pick $otherPath 0 ([:len $gateRoot] + 2)] = ("/" . $gateRoot . "/")) do={ :error "Verifier root in use" }
}
/file/remove $remainingRoot
}
:return true
}
:local authorizePanel do={
:local containerName $1
:local stateDir $2
:local routerIP $3
:local panelPort $4
:local attempt [:rndstr from="0123456789abcdef" length=32]
:local containers [/container/find where name=$containerName]
:if ([:len $containers] != 1) do={ :error "Meridian bootstrap container unavailable" }
:local m7InstallC [:pick $containers 0]
:if ([/container/get $m7InstallC logging] || [/container/get $m7InstallC start-on-boot]) do={ :error "Meridian bootstrap terminal configuration mismatch" }
:local mountList [:tostr [/container/get $m7InstallC mountlists]]
:local mounted [/container/mounts/find where list=$mountList]
:if ([:len $mounted] != 1) do={ :error "Meridian state mount mismatch" }
:local mountID [:pick $mounted 0]
:local source [:tostr [/container/mounts/get $mountID src]]
:if ([/container/mounts/get $mountID dst] != "/state" || ($source != ("/" . $stateDir) && $source != $stateDir)) do={ :error "Meridian state mount mismatch" }
:local gate ($stateDir . "/gate-" . $attempt . ".ok")
:local used ($stateDir . "/used-" . $attempt)
:local panel ($stateDir . "/ui-identity")
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 0) || ([:len [/file/find where name=$panel]] != 0)) do={ :error "Meridian installation state already exists" }
/container/shell $m7InstallC cmd=("/usr/local/bin/meridian-installer --authorize-install " . $attempt) no-sh
:if ([:len [/file/find where name=$gate]] != 1) do={ :error "Premium authorization incomplete" }
/container/shell $m7InstallC cmd=("/usr/local/bin/meridian-installer --prepare-panel " . $attempt . " " . [:tostr $routerIP] . " " . [:tostr $panelPort]) no-sh
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 1)) do={ :error "Premium authorization receipt not consumed" }
:foreach suffix in={"panel.crt";"panel.key";"preview.auth"} do={
:local obj [/file/find where name=($panel . "/" . $suffix)]
:if ([:len $obj] != 1) do={ :error "Panel identity incomplete" }
:if ([/file/get [:pick $obj 0] size] < 1) do={ :error "Panel identity incomplete" }
}
:return true
}
:local m7NetworkIDs [:toarray ""]
:local m7NetworkFingerprint do={
:local observed $1
:if ([:typeof $observed] != "array" || [:len [:tostr ($observed->".id")]] = 0) do={ :error "Unable to fingerprint network object" }
:foreach field in={"bytes";"packets";"invalid";"hw-offloaded"} do={ :set ($observed->$field) }
:return [:convert [:serialize to=json value=$observed options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local m7PlaceFilter do={
:local ident $1
:local label $2
:local first $3
:if ([/ip/firewall/filter/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/filter/move $ident destination=$first }
/ip/firewall/filter/enable $ident
:return ([/ip/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceFilter6 do={
:local ident $1
:local label $2
:local first $3
:if ([/ipv6/firewall/filter/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ipv6/firewall/filter/move $ident destination=$first }
/ipv6/firewall/filter/enable $ident
:return ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceNAT do={
:local ident $1
:local label $2
:local first $3
:if ([/ip/firewall/nat/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/nat/move $ident destination=$first }
/ip/firewall/nat/enable $ident
:return ([/ip/firewall/nat/print as-value where .id=$ident]->0)
}
:local m7ModeIDs [:toarray ""]
:local m7ModeV6
:local lockName ($flash . "meridian-install.lock")
:local lockID
:local releaseLock do={
:local lockFile $1
:local lockTag $2
:local lockPath $3
:local found [/file/find where name=$lockPath]
:if ([:len $found] != 1 || [:tostr [:pick $found 0]] != [:tostr $lockFile] || [/file/get $lockFile contents] != $lockTag) do={ :error "Meridian installation lock changed" }
/file/remove $lockFile
}
:local m7InstallC
:put ("Meridian installation " . $m7InstallName)
:onerror failure in={
/file/add name=$lockName type=file contents=$m7InstallTag
:local held [/file/find where name=$lockName]
:if ([:len $held] != 1 || [/file/get $held contents] != $m7InstallTag) do={ :error "Concurrent installer or interrupted installation" }
:set lockID [:pick $held 0]
:if ([:len [/container/find where comment~"^meridian-install:"]] > 0) do={ :error "Existing Meridian installation found" }
:put "Meridian: downloading key verifier"
:if (![$m7FetchImage $gateArchive "https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/meridian-hapax2-premium-edaa1c42aadd73aeebe44c8e5bebdb4aeddd5ea97c696186a706ba510b631fe9.tar" 6656000 "5d91af17e1c7293bfe17e7d6cc72bdb46d4fccedafd6a673e0ca46d952ecc6e56f65ebdfb5c493cfd942c2e00ba3e98edbcd8499a9679b4501c3545bd5115c1a" 28006400]) do={ :error "Verifier download failed" }
/interface/bridge/add name=$m7InstallBridge protocol-mode=none comment=$m7InstallOwner
:set ($m7GateState->"vethID") [/interface/veth/add name=$m7InstallVeth address=([:tostr $m7InstallBackend] . "/30") gateway=$m7InstallGateway comment=$m7InstallOwner]
/interface/bridge/port/add bridge=$m7InstallBridge interface=$m7InstallVeth comment=$m7InstallOwner
/ip/address/add address=([:tostr $m7InstallGateway] . "/30") interface=$m7InstallBridge comment=$m7InstallOwner
/interface/bridge/add name=$m7InstallLan protocol-mode=none comment=$acOwner
/interface/veth/add name=$acVeth address=([:tostr $acSource] . "/30") gateway=$routerIP comment=$acOwner
/interface/bridge/port/add bridge=$m7InstallLan interface=$acVeth comment=$acOwner
/ip/address/add address=([:tostr $routerIP] . "/30") interface=$m7InstallLan comment=$acOwner
{
:local label ($m7InstallOwner . "-ac-in-drop")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward out-interface=$m7InstallLan action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-out-drop")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-wan-in")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallWan out-interface=$m7InstallLan dst-address=$acSource connection-state=established,related action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-https")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan out-interface=$m7InstallWan src-address=$acSource protocol=tcp dst-port=443 action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-dns")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan out-interface=$m7InstallWan src-address=$acSource protocol=udp dst-port=53 action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-dns-tcp")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan out-interface=$m7InstallWan src-address=$acSource protocol=tcp dst-port=53 action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-ac-wan-nat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=srcnat out-interface=$m7InstallWan src-address=$acSource action=masquerade comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop-output")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward out-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop-input")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop-router")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=input in-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop6-router")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=input in-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop6-from")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward in-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-drop6-to")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward out-interface=$m7InstallBridge action=drop comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-wan-return")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallWan out-interface=$m7InstallBridge dst-address=$m7InstallBackend connection-state=established,related action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-wan-out")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallBridge out-interface=$m7InstallWan src-address=$m7InstallBackend action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-wan-nat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=srcnat out-interface=$m7InstallWan src-address=$m7InstallBackend action=masquerade comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
:set ($m7GateState->"mountID") [/container/mounts/add list=$m7InstallMount src=$stateDir dst=/state]
:local gateEnvID [/container/envs/add list=$gateEnv key=M7_DEVICE_ID value=$deviceID]
/container/add name=$gateName file=$gateArchive root-dir=$gateRoot interface=$m7InstallVeth mountlists=$m7InstallMount envlists=$gateEnv entrypoint="/usr/local/bin/meridian-installer" logging=no start-on-boot=no comment=$m7InstallOwner
:local gateRows [/container/find where name=$gateName and comment=$m7InstallOwner]
:if ([:len $gateRows] != 1) do={ :error "Verifier container creation uncertain" }
:local gateID [:pick $gateRows 0]
:local gateImageID "f68a45f4585944e13e68f19fdfc8824dd2c94e6d8e1cec9941132a7ef8e6762e"
:if (![$m7WaitImport $gateID $gateImageID]) do={ :error "Verifier import failed" }
:local rootFiles [/file/find where name=$gateRoot]
:local originalRootID
:if ([:len $rootFiles] = 1) do={ :set originalRootID [:pick $rootFiles 0] }
:set ($m7GateState->"gateID") $gateID
:set ($m7GateState->"gateName") $gateName
:set ($m7GateState->"owner") $m7InstallOwner
:set ($m7GateState->"mount") $m7InstallMount
:set ($m7GateState->"gateEnv") $gateEnv
:set ($m7GateState->"veth") $m7InstallVeth
:set ($m7GateState->"gateImageID") $gateImageID
:set ($m7GateState->"gateEnvID") $gateEnvID
:set ($m7GateState->"gateArchive") $gateArchive
:set ($m7GateState->"originalRootID") $originalRootID
:set ($m7GateState->"gateRoot") $gateRoot
:set ($m7GateState->"deviceID") $deviceID
:set ($m7GateState->"size") 6656000
:set ($m7GateState->"archiveID") [:pick [/file/find where name=$gateArchive] 0]
/container/start $gateID
:local bootReady ($stateDir . "/bootstrap-ready")
:for i from=1 to=40 do={ :if ([:len [/file/find where name=$bootReady]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$bootReady]] != 1) do={ :error "Verifier did not start" }
:if (![$authorizePanel $gateName $stateDir $routerIP $m7InstallPort]) do={ :error "Premium authorization failed" }
:if (![$m7RetireGate $m7GateState]) do={ :error "Verifier retirement failed" }
:set ($m7GateState->"retired") true
:if ([:len [/file/find where name=($stateDir . "/ui-identity/preview.auth")]] != 1) do={ :error "Authorized state unavailable" }
:put "Meridian: Premium verified; downloading VPN and panel"
:if (![$m7FetchImage $m7ImagePath "https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/meridian-hapax2-candidate-8f89d1248123b593890672ed082617488490e293c93df327782f32777128b184.tar" 17520640 "b97fd1b150685785126c75a1c5fdd3c70d157fbe65874bc2cfca79d53bf1f47dfbd19b47b31b15aa486b9ef5d2c46133caa0d2af268310844a3e7ada7952ed85" 70561280]) do={ :error "Runtime download failed" }
/container/envs/add list=$m7InstallEnv key=M7_INGRESS_IFACE value=$m7InstallVeth
/container/envs/add list=$m7InstallEnv key=M7_SOURCE_CIDR value=$clientNetwork
/container/envs/add list=$m7InstallEnv key=M7_PANEL_CIDR value=$clientNetwork
/container/envs/add list=$m7InstallEnv key=M7_PANEL_BIND value=([:tostr $m7InstallBackend] . ":" . $m7InstallPort)
/container/envs/add list=$m7InstallEnv key=M7_PANEL_AUTHORITY value=([:tostr $routerIP] . ":" . $m7InstallPort)
/container/envs/add list=$m7InstallEnv key=M7_READY_ID value=$m7InstallTag
/container/add name=$m7InstallName file=$m7ImagePath root-dir=$rootDir interface=$m7InstallVeth mountlists=$m7InstallMount envlists=$m7InstallEnv entrypoint="/usr/local/sbin/m7-image-entry" cmd=stack logging=no start-on-boot=no comment=$m7InstallOwner
:set m7InstallC [/container/find where name=$m7InstallName and comment=$m7InstallOwner]
:if ([:len $m7InstallC] != 1 || ![$m7WaitImport [:pick $m7InstallC 0] "d57f32457912c5a5b61e90e36f978a9277e9e004b310cdc4bd585fd10e728b03"]) do={ :error "VPN container import failed" }
:local stamp ($stateDir . "/stack-ready-" . $m7InstallTag)
:if ([:len [/file/find where name=$stamp]] != 0) do={ :error "Unexpected startup marker" }
/container/start $m7InstallC
:for i from=1 to=120 do={ :if ([:len [/file/find where name=$stamp]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$stamp]] != 1) do={ :error "VPN tunnel and panel startup unconfirmed" }
{
:local label ($m7InstallOwner . "-panel-reply")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallBridge out-interface=$m7InstallLan src-address=$m7InstallBackend dst-address=$clientNetwork protocol=tcp src-port=$m7InstallPort connection-state=established connection-nat-state=dstnat action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-panel-access")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan out-interface=$m7InstallBridge src-address=$clientNetwork dst-address=$m7InstallBackend protocol=tcp dst-port=$m7InstallPort connection-nat-state=dstnat action=accept comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local label ($m7InstallOwner . "-panel-dnat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=dstnat in-interface=$m7InstallLan src-address=$clientNetwork dst-address=$routerIP protocol=tcp dst-port=$m7InstallPort action=dst-nat to-addresses=$m7InstallBackend to-ports=$m7InstallPort comment=$label disabled=yes]
:set ($m7NetworkIDs->$label) $ident
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $label $first]
:set ($m7NetworkIDs->($label . "-sha")) [$m7NetworkFingerprint $actual]
}
{
:local fingerprint do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local table ("m7t" . $suffix)
:local black ("m7b" . $suffix)
:local white ("m7w" . $suffix)
:local marker ("m7-" . $suffix . "-")
:local scoped ("^m7-" . $suffix . "-")
:local hold4 [/ip/firewall/raw/find where comment~$scoped]
:local hold6 [/ipv6/firewall/raw/find where comment~$scoped]
:if ([:len [/routing/table/find where name=$table]]>0 || [:len [/ip/firewall/mangle/find where chain=$black or chain=$white]]>0 || [:len [/ip/firewall/mangle/find where comment~$scoped]]>0 || [:len $hold4]>0 || [:len $hold6]>0) do={ :error "Mode names in use" }
:local layoutPath ($stateDir . "/policy-layout.json")
:if ([:len [/file/find where name=$layoutPath]]>0) do={ :error "Policy layout already exists" }
:local count6 0
:foreach rid in=[/ipv6/route/find where active=yes and routing-table=main] do={
:if ([:tostr [/ipv6/route/get $rid gateway]] = [:tostr $m7InstallLan]) do={
:local prefix6 [/ipv6/route/get $rid dst-address]
:local text6 [:tostr $prefix6]
:local j [:find $text6 "/"]
:if ([:typeof $j] = "num") do={
:local host6 [:toip6 [:pick $text6 0 $j]]
:local b6 [:tonum [:pick $text6 ($j + 1) [:len $text6]]]
:if ([:typeof $host6] = "ip6" && $b6 >= 48 && $b6 <= 120 && !($host6 in fe80::/10) && !($host6 in ff00::/8)) do={
:set count6 ($count6 + 1)
:set m7ModeV6 $prefix6
}
}
}
}
:if ($count6 != 1) do={ :set m7ModeV6 }
:set ($m7ModeIDs->"table") [/routing/table/add name=$table fib comment=($m7InstallOwner . "-policy-table")]
:set ($m7ModeIDs->"sha-table") [$fingerprint ([/routing/table/print as-value where .id=($m7ModeIDs->"table")]->0)]
:set ($m7ModeIDs->"kill") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table blackhole distance=254 comment=($m7InstallOwner . "-policy-kill")]
:set ($m7ModeIDs->"sha-kill") [$fingerprint ([/ip/route/print as-value where .id=($m7ModeIDs->"kill")]->0)]
:set ($m7ModeIDs->"via") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table gateway=([:tostr $m7InstallBackend] . "@main") distance=1 comment=($m7InstallOwner . "-policy-via")]
:set ($m7ModeIDs->"sha-via") [$fingerprint ([/ip/route/print as-value where .id=($m7ModeIDs->"via")]->0)]
:set ($m7ModeIDs->"local-black") [/ip/firewall/mangle/add chain=$black dst-address-type=local action=accept comment=($marker . "local-black")]
:set ($m7ModeIDs->"sha-local-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"local-black")]->0)]
:set ($m7ModeIDs->"local-white") [/ip/firewall/mangle/add chain=$white dst-address-type=local action=accept comment=($marker . "local-white")]
:set ($m7ModeIDs->"sha-local-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"local-white")]->0)]
:set ($m7ModeIDs->"keep-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-black")]
:set ($m7ModeIDs->"sha-keep-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"keep-black")]->0)]
:set ($m7ModeIDs->"keep-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-white")]
:set ($m7ModeIDs->"sha-keep-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"keep-white")]->0)]
:set ($m7ModeIDs->"listed-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7b" . $suffix) action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "listed-black")]
:set ($m7ModeIDs->"sha-listed-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"listed-black")]->0)]
:set ($m7ModeIDs->"listed-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7d" . $suffix) action=accept comment=($marker . "listed-white")]
:set ($m7ModeIDs->"sha-listed-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"listed-white")]->0)]
:set ($m7ModeIDs->"default-white") [/ip/firewall/mangle/add chain=$white action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "default-white")]
:set ($m7ModeIDs->"sha-default-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"default-white")]->0)]
:local guardBlack ("m7gb" . $suffix)
:local guardWhite ("m7gw" . $suffix)
:local replyMark ("m7ri" . $suffix)
:if ([:len [/ip/firewall/filter/find where chain=$guardBlack or chain=$guardWhite]]>0) do={ :error "Policy guard chain collision" }
:set ($m7ModeIDs->"reply-new") [/ip/firewall/mangle/add chain=forward in-interface=$m7InstallWan out-interface=$m7InstallLan dst-address=$clientNetwork connection-state=new connection-mark=no-mark action=mark-connection new-connection-mark=$replyMark passthrough=yes disabled=yes comment=($marker . "reply-new")]
:set ($m7ModeIDs->"sha-reply-new") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"reply-new")]->0)]
:set ($m7ModeIDs->"reply-black") [/ip/firewall/mangle/add chain=$black connection-mark=$replyMark action=accept place-before=($m7ModeIDs->"keep-black") comment=($marker . "reply-black")]
:set ($m7ModeIDs->"sha-reply-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"reply-black")]->0)]
:set ($m7ModeIDs->"reply-white") [/ip/firewall/mangle/add chain=$white connection-mark=$replyMark action=accept place-before=($m7ModeIDs->"keep-white") comment=($marker . "reply-white")]
:set ($m7ModeIDs->"sha-reply-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"reply-white")]->0)]
:set ($m7ModeIDs->"gr-black") [/ip/firewall/filter/add chain=$guardBlack connection-mark=$replyMark action=return comment=($marker . "gr-black")]
:set ($m7ModeIDs->"sha-gr-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gr-black")]->0)]
:set ($m7ModeIDs->"gr-white") [/ip/firewall/filter/add chain=$guardWhite connection-mark=$replyMark action=return comment=($marker . "gr-white")]
:set ($m7ModeIDs->"sha-gr-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gr-white")]->0)]
:set ($m7ModeIDs->"gk-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-black")]
:set ($m7ModeIDs->"sha-gk-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gk-black")]->0)]
:set ($m7ModeIDs->"gk-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-white")]
:set ($m7ModeIDs->"sha-gk-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gk-white")]->0)]
:set ($m7ModeIDs->"gl-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7b" . $suffix) out-interface=("!" . $m7InstallBridge) action=drop comment=($marker . "gl-black")]
:set ($m7ModeIDs->"sha-gl-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gl-black")]->0)]
:set ($m7ModeIDs->"gl-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7d" . $suffix) action=return comment=($marker . "gl-white")]
:set ($m7ModeIDs->"sha-gl-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gl-white")]->0)]
:set ($m7ModeIDs->"gd-white") [/ip/firewall/filter/add chain=$guardWhite out-interface=("!" . $m7InstallBridge) action=drop comment=($marker . "gd-white")]
:set ($m7ModeIDs->"sha-gd-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gd-white")]->0)]
:local allForward [/ip/firewall/filter/find where chain=forward]
:set ($m7ModeIDs->"gentry") [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan src-address=$clientNetwork action=jump jump-target=$guardBlack disabled=yes place-before=[:pick $allForward 0] comment=($marker . "gentry")]
:set ($m7ModeIDs->"sha-gentry") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"gentry")]->0)]
:set ($m7ModeIDs->"lan-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$clientNetwork comment=($m7InstallOwner . "-policy-keep-lan")]
:set ($m7ModeIDs->"sha-lan-keep") [$fingerprint ([/ip/firewall/address-list/print as-value where .id=($m7ModeIDs->"lan-keep")]->0)]
:local linkAddr [/ip/address/find where interface=$m7InstallBridge and comment=$m7InstallOwner]
:if ([:len $linkAddr] != 1) do={ :error "VPN link address unavailable" }
:local linkNetwork [:tostr [/ip/address/get [:pick $linkAddr 0] network]]
:set ($m7ModeIDs->"link-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=($linkNetwork . "/30") comment=($m7InstallOwner . "-policy-keep-link")]
:set ($m7ModeIDs->"sha-link-keep") [$fingerprint ([/ip/firewall/address-list/print as-value where .id=($m7ModeIDs->"link-keep")]->0)]
:local n 0
:foreach aid in=[/ip/address/find where disabled=no and invalid=no] do={
:local iface [:tostr [/ip/address/get $aid interface]]
:if ($iface!=[:tostr $m7InstallWan] && $iface!=[:tostr $m7InstallBridge] && $iface!=[:tostr $m7InstallLan]) do={
:local a [:tostr [/ip/address/get $aid address]]
:local slash [:find $a "/"]
:if ([:typeof $slash]!="num") do={ :error "Incomplete connected subnet" }
:local bits [:tonum [:pick $a ($slash+1) [:len $a]]]
:if ($bits<8 || $bits>32 || $n>=128) do={ :error "Unsupported connected subnet" }
:local prefix ([:tostr [/ip/address/get $aid network]] . "/" . $bits)
:local keepName ("m7k" . $suffix)
:if ([:len [/ip/firewall/address-list/find where list=$keepName and address=$prefix]]>0) do={ :continue }
:local key ("connected-" . $n)
:local item [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$prefix comment=($m7InstallOwner . "-" . $key)]
:set ($m7ModeIDs->$key) $item
:set ($m7ModeIDs->("sha-" . $key)) [$fingerprint ([/ip/firewall/address-list/print as-value where .id=$item]->0)]
:set n ($n+1)
}
}
:set ($m7ModeIDs->"entry") [/ip/firewall/mangle/add chain=prerouting in-interface=$m7InstallLan src-address=$clientNetwork action=jump jump-target=$black disabled=yes comment=($marker . "entry")]
:set ($m7ModeIDs->"sha-entry") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($m7ModeIDs->"entry")]->0)]
:local denyOut [/ip/firewall/filter/find where comment=($m7InstallOwner . "-drop-output")]
:local denyIn [/ip/firewall/filter/find where comment=($m7InstallOwner . "-drop-input")]
:if ([:len $denyOut] != 1 || [:len $denyIn] != 1) do={ :error "Missing container boundary" }
:set ($m7ModeIDs->"vpn-out") [/ip/firewall/filter/add chain=forward in-interface=$m7InstallLan out-interface=$m7InstallBridge src-address=$clientNetwork dst-address=("!" . [:tostr $m7InstallBackend]) routing-mark=$table action=accept place-before=[:pick $denyOut 0] disabled=yes comment=($m7InstallOwner . "-policy-vpn-out")]
:set ($m7ModeIDs->"sha-vpn-out") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"vpn-out")]->0)]
:set ($m7ModeIDs->"vpn-in") [/ip/firewall/filter/add chain=forward in-interface=$m7InstallBridge out-interface=$m7InstallLan src-address=("!" . [:tostr $m7InstallBackend]) dst-address=$clientNetwork connection-state=established,related action=accept place-before=[:pick $denyIn 0] disabled=yes comment=($m7InstallOwner . "-policy-vpn-in")]
:set ($m7ModeIDs->"sha-vpn-in") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"vpn-in")]->0)]
:set ($m7ModeIDs->"panel-reject") [/ip/firewall/filter/add chain=forward in-interface=$m7InstallBridge out-interface=$m7InstallLan src-address=$m7InstallBackend dst-address=$clientNetwork protocol=tcp src-port=$m7InstallPort action=drop place-before=[:pick $denyIn 0] disabled=yes comment=($m7InstallOwner . "-policy-panel-reject")]
:set ($m7ModeIDs->"sha-panel-reject") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($m7ModeIDs->"panel-reject")]->0)]
:set ($m7ModeIDs->"panel-out") [/ip/firewall/raw/add chain=prerouting in-interface=$m7InstallLan src-address=$clientNetwork dst-address=$routerIP protocol=tcp dst-port=$m7InstallPort action=accept disabled=yes comment=($m7InstallOwner . "-policy-panel-out")]
:set ($m7ModeIDs->"sha-panel-out") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($m7ModeIDs->"panel-out")]->0)]
:set ($m7ModeIDs->"panel-in") [/ip/firewall/raw/add chain=prerouting in-interface=$m7InstallBridge src-address=$m7InstallBackend dst-address=$clientNetwork protocol=tcp src-port=$m7InstallPort action=accept disabled=yes comment=($m7InstallOwner . "-policy-panel-in")]
:set ($m7ModeIDs->"sha-panel-in") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($m7ModeIDs->"panel-in")]->0)]
:set ($m7ModeIDs->"router-in") [/ip/firewall/raw/add chain=prerouting in-interface=$m7InstallLan src-address=$clientNetwork dst-address=$routerIP action=accept disabled=yes comment=($m7InstallOwner . "-policy-router-in")]
:set ($m7ModeIDs->"sha-router-in") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($m7ModeIDs->"router-in")]->0)]
:set ($m7ModeIDs->"out4") [/ip/firewall/raw/add chain=prerouting src-address=$clientNetwork action=drop disabled=yes comment=($marker . "out4")]
:set ($m7ModeIDs->"sha-out4") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($m7ModeIDs->"out4")]->0)]
:set ($m7ModeIDs->"in4") [/ip/firewall/raw/add chain=prerouting dst-address=$clientNetwork action=drop disabled=yes comment=($marker . "in4")]
:set ($m7ModeIDs->"sha-in4") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($m7ModeIDs->"in4")]->0)]
:if ($count6 = 1) do={
:set ($m7ModeIDs->"out6") [/ipv6/firewall/raw/add chain=prerouting src-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "out6")]
:set ($m7ModeIDs->"sha-out6") [$fingerprint ([/ipv6/firewall/raw/print as-value where .id=($m7ModeIDs->"out6")]->0)]
:set ($m7ModeIDs->"in6") [/ipv6/firewall/raw/add chain=prerouting dst-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "in6")]
:set ($m7ModeIDs->"sha-in6") [$fingerprint ([/ipv6/firewall/raw/print as-value where .id=($m7ModeIDs->"in6")]->0)]
}
:set ($m7ModeIDs->"v6-from") [/ipv6/firewall/filter/add chain=forward in-interface=$m7InstallLan action=drop disabled=yes comment=($m7InstallOwner . "-policy-v6-from")]
:set ($m7ModeIDs->"sha-v6-from") [$fingerprint ([/ipv6/firewall/filter/print as-value where .id=($m7ModeIDs->"v6-from")]->0)]
:set ($m7ModeIDs->"v6-to") [/ipv6/firewall/filter/add chain=forward out-interface=$m7InstallLan action=drop disabled=yes comment=($m7InstallOwner . "-policy-v6-to")]
:set ($m7ModeIDs->"sha-v6-to") [$fingerprint ([/ipv6/firewall/filter/print as-value where .id=($m7ModeIDs->"v6-to")]->0)]
:local layout [:toarray ""]
:set ($layout->"version") 1
:set ($layout->"phase") "staged"
:set ($layout->"tag") $m7InstallTag
:set ($layout->"mode_tag") $suffix
:set ($layout->"source") $clientNetwork
:set ($layout->"ipv6") [:tostr $m7ModeV6]
:set ($layout->"lan") $m7InstallLan
:set ($layout->"link") $m7InstallBridge
:set ($layout->"veth") $m7InstallVeth
:set ($layout->"wan") $m7InstallWan
:set ($layout->"backend") [:tostr $m7InstallBackend]
:set ($layout->"router") [:tostr $routerIP]
:set ($layout->"port") $m7InstallPort
:local refs [:toarray ""]
:foreach k,id in=$m7ModeIDs do={ :if ([:pick $k 0 4] != "sha-") do={ :set ($refs->$k) [:tostr $id] } }
:set ($layout->"objects") $refs
:local body [:serialize to=json value=$layout options=json.no-string-conversion]
/file/add name=$layoutPath type=file contents=$body
:local created [/file/find where name=$layoutPath]
:if ([:len $created] != 1 || [/file/get [:pick $created 0] contents] != $body) do={ :error "Policy layout not persisted" }
:set ($m7ModeIDs->"layout") [:pick $created 0]
:set ($m7ModeIDs->"digest") [:convert $body from=raw transform=sha512 to=hex]
:local hostLayout ($flash . "m7h" . $suffix . ".json")
:local hostState ($flash . "m7x" . $suffix . ".json")
:local hostCtl ("m7ctl" . $suffix)
:if ([:len [/file/find where name=$hostLayout or name=$hostState]]>0 || [:len [/system/script/find where name=$hostCtl]]>0 || [:len [/system/scheduler/find where name=$hostCtl]]>0) do={ :error "Policy controller ownership collision" }
/file/add name=$hostLayout type=file contents=$body
:set ($m7ModeIDs->"host-layout") [:pick [/file/find where name=$hostLayout] 0]
:local initial "{\"phase\":\"stable\",\"mode\":\"off\",\"digest\":\"\",\"list\":\"\",\"last\":\"\"}"
/file/add name=$hostState type=file contents=$initial
:set ($m7ModeIDs->"host-state") [:pick [/file/find where name=$hostState] 0]
:set ($m7ModeIDs->"initial") $initial
:local fetched [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/native-mode-dispatch.rsc" output=user as-value check-certificate=yes http-max-redirect-count=0 idle-timeout=30s]
:local program ($fetched->"data")
:if (($fetched->"status")!="finished" || [:convert $program from=raw transform=sha512 to=hex]!="2715b8cafa82548e66b20dee25afeb1360fa38c20ca3dd173b511a5fa9f5644176e75b8963e774cb8e730baaeb1ed6773b56a97a6301038ff5fd7d0b68bd364b") do={ :error "Policy controller checksum mismatch" }
:set ($m7ModeIDs->"script") [/system/script/add name=$hostCtl source=$program policy=ftp,read,write,test,sensitive comment=($m7InstallOwner . "-policy-controller")]
:set ($m7ModeIDs->"scheduler") [/system/scheduler/add name=$hostCtl interval=10s start-date=[/system/clock/get date] start-time=[/system/clock/get time] on-event=$hostCtl policy=ftp,read,write,test,sensitive comment=($m7InstallOwner . "-policy-controller")]
:put ("M7POLICY staged source=" . $clientNetwork . " link=" . $m7InstallBridge)
}
:local acBaseline [:toarray ""]
:local acFingerprint do={
:local observed $1
:local role $2
:foreach f in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($observed->$f) }
:if ([:len $role]>0) do={
:set ($observed->"disabled")
:if ($role="entry" || $role="gentry") do={ :set ($observed->"jump-target") }
:if ($role="gl-black" || $role="gl-white" || $role="listed-black" || $role="listed-white") do={ :set ($observed->"dst-address-list") }
}
:return [:convert [:serialize to=json value=$observed options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:foreach acRole in={"entry";"reply-new";"listed-black";"listed-white"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:set ($acBaseline->$acRole) [$acFingerprint $fields $acRole]
}
}
:foreach acRole in={"gentry";"gl-black";"gl-white";"vpn-out";"vpn-in";"panel-reject"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:set ($acBaseline->$acRole) [$acFingerprint $fields $acRole]
}
}
:foreach acRole in={"in4";"out4";"panel-in";"panel-out";"router-in"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:set ($acBaseline->$acRole) [$acFingerprint $fields $acRole]
}
}
:foreach acRole in={"v6-from";"v6-to"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:set ($acBaseline->$acRole) [$acFingerprint $fields $acRole]
}
}
:foreach acRole in={"in6";"out6"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:set ($acBaseline->$acRole) [$acFingerprint $fields $acRole]
}
}
:local acScheduled ("m7ctl" . $suffix)
:local acWorker [/system/script/find where name=$acScheduled and comment=($m7InstallOwner . "-policy-controller")]
:local acTimer [/system/scheduler/find where name=$acScheduled and comment=($m7InstallOwner . "-policy-controller")]
:local acState ($flash . "m7x" . $suffix . ".json")
:local acHostStatus ($stateDir . "/policy-host-status.json")
:local acDir ($flash . "acd" . $acNonce)
:local acRoot ($flash . $acContainer)
:local acTar ($acRoot . ".tar")
:if ([:len $acWorker]!=1 || [:len $acTimer]!=1 || [/system/scheduler/get $acTimer on-event]!=$acScheduled) do={ :error "M7ACCEPT ambiguous controller" }
:foreach f in=($acDir,$acRoot,$acTar) do={ :if ([:len [/file/find where name=$f]]!=0) do={ :error "M7ACCEPT probe path collision" } }
:local acMembers [/interface/bridge/port/find where bridge=$m7InstallLan]
:if ([:len $acMembers]!=1 || [/interface/bridge/port/get $acMembers interface]!=$acVeth || [/interface/bridge/port/get $acMembers comment]!=$acOwner || [/interface/bridge/get $m7InstallLan comment]!=$acOwner) do={ :error "M7ACCEPT isolated LAN ownership changed" }
:if (![$m7FetchImage $acTar "https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/probe-arm64.oci.tar" 1996800 "53ea4c9371a4766fab1a439e2d728f51a29ab5dfab8271be01cdd56d4c201c925e5a12dffc5f228a91d868317c578cee1f2e34f4874a04c16e1b70667f40231d" 21118976]) do={ :error "M7ACCEPT probe import preflight" }
/file/add name=$acDir type=directory
:foreach f in={"preview.auth";"panel.crt"} do={
:local original [/file/find where name=($stateDir . "/ui-identity/" . $f)]
:if ([:len $original]!=1 || [/file/get $original size]<1 || [/file/get $original size]>4096) do={ :error "M7ACCEPT panel identity unavailable" }
:local copied [/file/add name=($acDir . "/" . $f) type=file contents=[/file/get $original contents]]
:if ($f="preview.auth") do={ :set acAuthReceipt $copied }
}
/container/mounts/add list=$acMount src=$acDir dst=/check
/container/envs/add list=$acEnv key=M7_PROBE_ROUTER value=[:tostr $routerIP]
/container/envs/add list=$acEnv key=M7_PROBE_SOURCE value=[:tostr $acSource]
/container/add name=$acContainer file=$acTar interface=$acVeth root-dir=$acRoot mountlists=$acMount envlists=$acEnv logging=yes start-on-boot=no comment=$acOwner
:local acProbe [/container/find where name=$acContainer and comment=$acOwner]
:if ([:len $acProbe]!=1 || ![$m7WaitImport [:pick $acProbe 0] "00d03a65bccafa49f07c2f04f16814ed5cca237210aa8564caed920dc1c1ab67"]) do={ :error "M7ACCEPT probe OCI identity" }
:local acFiles [:toarray ""]
:foreach acPath in=($rootDir,$stateDir,$m7ImagePath,$acDir,$acRoot,$acTar) do={
:local files [/file/find where name=$acPath]
:if ([:len $files]!=1) do={ :error "M7ACCEPT path receipt unavailable" }
:set ($acFiles->$acPath) [:tostr [:pick $files 0]]
}
:local acEnvs [:toarray ""]
:foreach entry in=[/container/envs/find where list=$m7InstallEnv or list=$acEnv] do={
:local row ([/container/envs/print as-value where .id=$entry]->0)
:set ($acEnvs->[:tostr $entry]) [$acFingerprint $row ""]
}
:local acMounts [:toarray ""]
:foreach entry in=[/container/mounts/find where list=$m7InstallMount or list=$acMount] do={
:local row ([/container/mounts/print as-value where .id=$entry]->0)
:set ($acMounts->[:tostr $entry]) [$acFingerprint $row ""]
}
/container/start $acProbe
:local acWait do={
:local p $1
:for i from=1 to=$2 do={ :if ([:len [/file/find where name=$p]]=1 && [/file/get [find where name=$p] contents]="ok") do={ :return true }; :delay 1s }
:return false
}
:if (![$acWait ($acDir . "/white-confirmed") 205]) do={ :error "M7ACCEPT panel or egress matrix unconfirmed" }
:put "M7ACCEPT panel black white verified on isolated LAN"
/system/scheduler/disable $acTimer
:for i from=1 to=20 do={ :if ([:len [/system/script/job/find where script=$acScheduled]]=0) do={ :break }; :delay 1s }
:if ([:len [/system/script/job/find where script=$acScheduled]]!=0) do={ :error "M7ACCEPT controller busy" }
:local acDecode do={
:local row [/file/find where name=$1]
:if ([:len $row]!=1) do={ :error "M7ACCEPT status missing" }
:return [:deserialize from=json value=[/file/get $row contents] options=json.no-string-conversion]
}
:local previous [$acDecode $acState]
:if (($previous->"phase")!="stable" || ($previous->"mode")!="white" || ($previous->"verification")!="applied") do={ :error "M7ACCEPT white rollback baseline unavailable" }
/file/add name=($acDir . "/scheduler-held") type=file contents="ok"
:if (![$acWait ($acDir . "/black-requested") 30]) do={ :error "M7ACCEPT test request unavailable" }
:local req
:for i from=1 to=10 do={
:set req [$acDecode ($stateDir . "/policy-request.json")]
:if (($req->"mode")="black" && ($req->"owner")=$m7InstallTag && ($req->"mode_revision")=3) do={ :break }
:delay 1s
}
:if (($req->"mode")!="black" || ($req->"mode_revision")!=3 || ($req->"owner")!=$m7InstallTag) do={ :error "M7ACCEPT stale request" }
:onerror rc in={ /system/script/run $acScheduled } do={
:if ([:tostr $rc]!="0" && [:tostr $rc]!="Script Error: 0") do={ :error $rc }
}
:local tx [$acDecode $acState]
:if (($tx->"phase")!="waiting" || ($tx->"mode")!="white" || (($tx->"next")->"request_id")!=($req->"request_id")) do={ :error "M7ACCEPT rollback was not armed" }
:if ([/container/get $m7InstallC comment]!=$m7InstallOwner || [/container/get $m7InstallC name]!=$m7InstallName) do={ :error "M7ACCEPT runtime ownership changed" }
/container/stop $m7InstallC
:local stopped false
:for i from=1 to=40 do={ :local list [/container/print as-value where name=$m7InstallName and comment=$m7InstallOwner]; :if ([:len $list]=1 && [:tostr (($list->0)->"stopped")]="true") do={ :set stopped true; :break }; :delay 1s }
:if (!$stopped) do={ :error "M7ACCEPT unable to induce isolated runtime failure" }
:onerror rc in={ /system/script/run $acScheduled } do={
:if ([:tostr $rc]!="0" && [:tostr $rc]!="Script Error: 0") do={ :error $rc }
}
:local result [$acDecode $acHostStatus]
:local restored [$acDecode $acState]
:if (($result->"owner")!=$m7InstallTag || ($result->"request_id")!=($req->"request_id") || ($result->"status")!="rolled-back" || ($restored->"mode")!="white" || ($restored->"phase")!="stable" || ($restored->"digest")!=($previous->"digest")) do={ :error "M7ACCEPT rollback readback failed" }
/container/start $m7InstallC
:for i from=1 to=120 do={ :if ([:len [/file/find where name=$stamp]]=1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$stamp]]!=1) do={ :error "M7ACCEPT runtime recovery unconfirmed" }
/file/add name=($acDir . "/runtime-restored") type=file contents="ok"
:if (![$acWait ($acDir . "/verified") 100]) do={ :error "M7ACCEPT rollback traffic path unconfirmed" }
:put "M7ACCEPT rollback verified on isolated LAN"
:if ([:typeof $acAuthReceipt]="id") do={
:local copies [/file/find where name=($flash . "acd" . $acNonce . "/preview.auth")]
:if ([:len $copies]=1 && [:tostr [:pick $copies 0]]=[:tostr $acAuthReceipt]) do={
:local original [/file/find where name=($stateDir . "/ui-identity/preview.auth")]
:if ([:len $original]=1 && [/file/get $acAuthReceipt contents]=[/file/get $original contents]) do={ /file/remove $acAuthReceipt }
}
}
:local acPort [/interface/bridge/port/find where bridge=$m7InstallLan and interface=$acVeth and comment=$acOwner]
:if ([:len $acPort]!=1 || [:len [/interface/bridge/port/find where bridge=$m7InstallLan]]!=1) do={ :error "M7ACCEPT probe ownership changed" }
/interface/bridge/port/disable $acPort
/container/stop $m7InstallC
:onerror done in={ /container/stop $acProbe } do={}
:if ([:len [/system/script/job/find where script=$acScheduled]]!=0 || ![/system/scheduler/get $acTimer disabled]) do={ :error "M7ACCEPT controller still active" }
:local acNow [$acDecode $acState]
:if (($acNow->"phase")!="stable" || ($acNow->"digest")!=($previous->"digest") || ($acNow->"last")!=($req->"request_id") || ($acNow->"mode")!="white") do={ :error "M7ACCEPT snapshot changed" }
:set ($m7ModeIDs->"initial") [/file/get [find where name=$acState] contents]
:local activeList ($acNow->"list")
:local ownLabel ($m7InstallOwner . "-generation-" . $activeList)
:local listed [/ip/firewall/address-list/find where list=$activeList]
:if ([:len $listed]!=1 || [/ip/firewall/address-list/get $listed dynamic] || [/ip/firewall/address-list/get $listed comment]!=$ownLabel || [:len [/ip/dns/static/find where comment=$ownLabel]]!=0) do={ :error "M7ACCEPT list generation changed" }
:local val [:tostr [/ip/firewall/address-list/get $listed address]]
:if ([:len ($req->"prefixes")]!=1 || ($val!= (($req->"prefixes")->0) && ($val . "/32")!= (($req->"prefixes")->0))) do={ :error "M7ACCEPT list entry drift" }
:foreach acRole in={"entry";"reply-new";"listed-black";"listed-white"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:if ([$acFingerprint $fields $acRole]!=($acBaseline->$acRole)) do={ :error ("M7ACCEPT firewall ownership changed " . $acRole) }
:if ($acRole="entry" || $acRole="gentry") do={
:local prefix "m7"
:if ($acRole="gentry") do={ :set prefix "m7g" }
:local target ($fields->"jump-target")
:if ($target!=($prefix . "b" . $suffix) && $target!=($prefix . "w" . $suffix)) do={ :error "M7ACCEPT foreign jump target" }
}
:if ($acRole="gl-black" || $acRole="gl-white" || $acRole="listed-black" || $acRole="listed-white") do={
:local list ($fields->"dst-address-list")
:if ($list!=($tx->"next_list") && $list!=($previous->"list") && $list!=($previous->"next_list") && $list!=("m7b" . $suffix) && $list!=("m7d" . $suffix)) do={ :error "M7ACCEPT foreign destination list" }
}
:set ($m7ModeIDs->("sha-" . $acRole)) [$acFingerprint $fields ""]
}
}
:foreach acRole in={"gentry";"gl-black";"gl-white";"vpn-out";"vpn-in";"panel-reject"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:if ([$acFingerprint $fields $acRole]!=($acBaseline->$acRole)) do={ :error ("M7ACCEPT firewall ownership changed " . $acRole) }
:if ($acRole="entry" || $acRole="gentry") do={
:local prefix "m7"
:if ($acRole="gentry") do={ :set prefix "m7g" }
:local target ($fields->"jump-target")
:if ($target!=($prefix . "b" . $suffix) && $target!=($prefix . "w" . $suffix)) do={ :error "M7ACCEPT foreign jump target" }
}
:if ($acRole="gl-black" || $acRole="gl-white" || $acRole="listed-black" || $acRole="listed-white") do={
:local list ($fields->"dst-address-list")
:if ($list!=($tx->"next_list") && $list!=($previous->"list") && $list!=($previous->"next_list") && $list!=("m7b" . $suffix) && $list!=("m7d" . $suffix)) do={ :error "M7ACCEPT foreign destination list" }
}
:set ($m7ModeIDs->("sha-" . $acRole)) [$acFingerprint $fields ""]
}
}
:foreach acRole in={"in4";"out4";"panel-in";"panel-out";"router-in"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:if ([$acFingerprint $fields $acRole]!=($acBaseline->$acRole)) do={ :error ("M7ACCEPT firewall ownership changed " . $acRole) }
:if ($acRole="entry" || $acRole="gentry") do={
:local prefix "m7"
:if ($acRole="gentry") do={ :set prefix "m7g" }
:local target ($fields->"jump-target")
:if ($target!=($prefix . "b" . $suffix) && $target!=($prefix . "w" . $suffix)) do={ :error "M7ACCEPT foreign jump target" }
}
:if ($acRole="gl-black" || $acRole="gl-white" || $acRole="listed-black" || $acRole="listed-white") do={
:local list ($fields->"dst-address-list")
:if ($list!=($tx->"next_list") && $list!=($previous->"list") && $list!=($previous->"next_list") && $list!=("m7b" . $suffix) && $list!=("m7d" . $suffix)) do={ :error "M7ACCEPT foreign destination list" }
}
:set ($m7ModeIDs->("sha-" . $acRole)) [$acFingerprint $fields ""]
}
}
:foreach acRole in={"v6-from";"v6-to"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:if ([$acFingerprint $fields $acRole]!=($acBaseline->$acRole)) do={ :error ("M7ACCEPT firewall ownership changed " . $acRole) }
:if ($acRole="entry" || $acRole="gentry") do={
:local prefix "m7"
:if ($acRole="gentry") do={ :set prefix "m7g" }
:local target ($fields->"jump-target")
:if ($target!=($prefix . "b" . $suffix) && $target!=($prefix . "w" . $suffix)) do={ :error "M7ACCEPT foreign jump target" }
}
:if ($acRole="gl-black" || $acRole="gl-white" || $acRole="listed-black" || $acRole="listed-white") do={
:local list ($fields->"dst-address-list")
:if ($list!=($tx->"next_list") && $list!=($previous->"list") && $list!=($previous->"next_list") && $list!=("m7b" . $suffix) && $list!=("m7d" . $suffix)) do={ :error "M7ACCEPT foreign destination list" }
}
:set ($m7ModeIDs->("sha-" . $acRole)) [$acFingerprint $fields ""]
}
}
:foreach acRole in={"in6";"out6"} do={
:local acID ($m7ModeIDs->$acRole)
:if ([:typeof $acID]="id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$acID]
:if ([:len $rows]!=1) do={ :error "M7ACCEPT missing firewall object" }
:local fields ($rows->0)
:if ([$acFingerprint $fields $acRole]!=($acBaseline->$acRole)) do={ :error ("M7ACCEPT firewall ownership changed " . $acRole) }
:if ($acRole="entry" || $acRole="gentry") do={
:local prefix "m7"
:if ($acRole="gentry") do={ :set prefix "m7g" }
:local target ($fields->"jump-target")
:if ($target!=($prefix . "b" . $suffix) && $target!=($prefix . "w" . $suffix)) do={ :error "M7ACCEPT foreign jump target" }
}
:if ($acRole="gl-black" || $acRole="gl-white" || $acRole="listed-black" || $acRole="listed-white") do={
:local list ($fields->"dst-address-list")
:if ($list!=($tx->"next_list") && $list!=($previous->"list") && $list!=($previous->"next_list") && $list!=("m7b" . $suffix) && $list!=("m7d" . $suffix)) do={ :error "M7ACCEPT foreign destination list" }
}
:set ($m7ModeIDs->("sha-" . $acRole)) [$acFingerprint $fields ""]
}
}
/ip/firewall/address-list/remove $listed
{
:local fingerprint do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local hostState ($m7ModeIDs->"host-state")
:if ([:typeof $hostState]="id") do={
:if ([/file/get $hostState contents]!=($m7ModeIDs->"initial")) do={ :error "Active host policy requires reversal before cleanup" }
}
:local worker ("m7ctl" . $suffix)
:local schedulerID ($m7ModeIDs->"scheduler")
:if ([:typeof $schedulerID]="id") do={
:if ([/system/scheduler/get $schedulerID comment]!=($m7InstallOwner . "-policy-controller") || [/system/scheduler/get $schedulerID name]!=$worker || [:tostr [/system/scheduler/get $schedulerID on-event]]!=$worker) do={ :error "Scheduler ownership changed" }
/system/scheduler/remove $schedulerID
}
:local workerID ($m7ModeIDs->"script")
:if ([:typeof $workerID]="id") do={
:if ([:len [/system/script/job/find where script=$worker]]>0 || [/system/script/get $workerID comment]!=($m7InstallOwner . "-policy-controller") || [/system/script/get $workerID name]!=$worker) do={ :error "Policy worker is busy or changed" }
/system/script/remove $workerID
}
:local hostConfig ($m7ModeIDs->"host-layout")
:if ([:typeof $hostConfig]="id") do={
:if ([:convert [/file/get $hostConfig contents] from=raw transform=sha512 to=hex]!=($m7ModeIDs->"digest")) do={ :error "Host policy layout changed" }
/file/remove $hostConfig
}
:if ([:typeof $hostState]="id") do={
/file/remove $hostState
}
:local f ($m7ModeIDs->"layout")
:if ([:typeof $f] = "id") do={
:local files [/file/find where name=($stateDir . "/policy-layout.json")]
:if ([:len $files] != 1 || [:tostr [:pick $files 0]] != [:tostr $f]) do={ :error "Policy layout ownership changed" }
:if ([:convert [/file/get $f contents] from=raw transform=sha512 to=hex] != ($m7ModeIDs->"digest")) do={ :error "Policy layout changed" }
/file/remove $f
}
:foreach role in={"v6-to";"v6-from"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/filter/remove $id
}
}
:foreach role in={"in6";"out6"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/raw/remove $id
}
}
:foreach role in={"in4";"out4";"router-in";"panel-in";"panel-out"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/raw/remove $id
}
}
:foreach role in={"gentry";"gd-white";"gl-white";"gl-black";"gk-white";"gk-black";"gr-white";"gr-black";"panel-reject";"vpn-in";"vpn-out"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/filter/remove $id
}
}
:foreach role in={"entry";"reply-white";"reply-black";"reply-new";"default-white";"listed-white";"listed-black";"keep-white";"keep-black";"local-white";"local-black"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/mangle/remove $id
}
}
:foreach role,id in=$m7ModeIDs do={
:if ([:pick $role 0 10]="connected-" && [:typeof $id]="id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows]!=1 || [$fingerprint ($rows->0)]!=($m7ModeIDs->("sha-" . $role))) do={ :error "Connected network ownership changed" }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"link-keep";"lan-keep"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"via";"kill"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/route/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/route/remove $id
}
}
:foreach role in={"table"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/routing/table/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/routing/table/remove $id
}
}
}
:foreach part in={"panel-dnat";"wan-nat"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/nat/remove $ident
}
}
:foreach part in={"panel-access";"panel-reply";"wan-out";"wan-return";"drop-router";"drop-input";"drop-output"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/filter/remove $ident
}
}
:foreach part in={"drop6-to";"drop6-from";"drop6-router"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ipv6/firewall/filter/remove $ident
}
}
:foreach part in={"ac-in-drop";"ac-out-drop";"ac-wan-in";"ac-https";"ac-dns";"ac-dns-tcp"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows]!=1 || (($rows->0)->"comment")!=$label || [$m7NetworkFingerprint ($rows->0)]!=($m7NetworkIDs->($label . "-sha"))) do={ :error "Acceptance WAN rule changed" }
/ip/firewall/filter/remove $ident
}
:foreach part in={"ac-wan-nat"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows]!=1 || (($rows->0)->"comment")!=$label || [$m7NetworkFingerprint ($rows->0)]!=($m7NetworkIDs->($label . "-sha"))) do={ :error "Acceptance WAN rule changed" }
/ip/firewall/nat/remove $ident
}
:foreach cname in=($acContainer,$m7InstallName) do={
:local ident $acProbe
:local expectedOwner $acOwner
:local expectedImage "00d03a65bccafa49f07c2f04f16814ed5cca237210aa8564caed920dc1c1ab67"
:local expectedIf $acVeth
:local expectedMount $acMount
:if ($cname=$m7InstallName) do={ :set ident $m7InstallC; :set expectedOwner $m7InstallOwner; :set expectedImage "d57f32457912c5a5b61e90e36f978a9277e9e004b310cdc4bd585fd10e728b03"; :set expectedIf $m7InstallVeth; :set expectedMount $m7InstallMount }
:local acRemoving false
:for count from=1 to=40 do={
:local rows [/container/print as-value where name=$cname]
:if ([:len $rows]=0) do={ :break }
:local row ($rows->0)
:if ([:len $rows]!=1 || [:tostr ($row->".id")]!=[:tostr [:pick $ident 0]]) do={ :error "M7ACCEPT container identifier changed" }
:if (!$acRemoving) do={
:local rid [:pick $ident 0]
:if ([/container/get $rid image-id]!=$expectedImage) do={ :error "M7ACCEPT container image changed" }
:if ([/container/get $rid comment]!=$expectedOwner) do={ :error "M7ACCEPT container owner changed" }
:if ([:tostr [/container/get $rid interface]]!=$expectedIf) do={ :error "M7ACCEPT container interface changed" }
:if ([:tostr [/container/get $rid mountlists]]!=$expectedMount) do={ :error "M7ACCEPT container mount changed" }
}
:if (!$acRemoving && [:tostr ($row->"stopped")]="true") do={ /container/remove [:pick $ident 0]; :set acRemoving true }
:delay 1s
}
:if ([:len [/container/find where name=$cname]]!=0) do={ :error "M7ACCEPT cleanup uncertain" }
}
:foreach entry in=[/container/envs/find where list=$m7InstallEnv or list=$acEnv] do={
:if ([$acFingerprint ([/container/envs/print as-value where .id=$entry]->0) ""]!=($acEnvs->[:tostr $entry])) do={ :error "M7ACCEPT foreign environment" }
/container/envs/remove $entry
}
:foreach entry in=[/container/mounts/find where list=$m7InstallMount or list=$acMount] do={
:if ([$acFingerprint ([/container/mounts/print as-value where .id=$entry]->0) ""]!=($acMounts->[:tostr $entry])) do={ :error "M7ACCEPT foreign mount" }
/container/mounts/remove $entry
}
:foreach acBridge in=($m7InstallLan,$m7InstallBridge) do={
:local acIface $acVeth
:local acAddress ([:tostr $routerIP] . "/30")
:local acLabel $acOwner
:if ($acBridge=$m7InstallBridge) do={ :set acIface $m7InstallVeth; :set acLabel $m7InstallOwner; :set acAddress ([:tostr $m7InstallGateway] . "/30") }
:local ids [/interface/bridge/port/find where bridge=$acBridge]
:local addresses [/ip/address/find where interface=$acBridge]
:if ([:len $ids]!=1 || [/interface/bridge/port/get $ids interface]!=$acIface || [/interface/bridge/port/get $ids comment]!=$acLabel || [:len $addresses]!=1 || [/ip/address/get $addresses comment]!=$acLabel || [:tostr [/ip/address/get $addresses address]]!=$acAddress || [/interface/veth/get $acIface comment]!=$acLabel || [/interface/bridge/get $acBridge comment]!=$acLabel || [:len [/ipv6/address/find where interface=$acBridge and dynamic=no]]!=0) do={ :error "M7ACCEPT transit ownership changed" }
/ip/address/remove $addresses
/interface/bridge/port/remove $ids
/interface/veth/remove [find where name=$acIface and comment=$acLabel]
/interface/bridge/remove [find where name=$acBridge and comment=$acLabel]
}
:local norm do={ :local val [:tostr $1]; :if ([:pick $val 0 1]="/") do={ :set val [:pick $val 1 [:len $val]] }; :return ($val . "/") }
:foreach acPath,acID in=$acFiles do={
:local files [/file/find where name=$acPath]
:if ([:len $files]>0) do={
:if ([:len $files]!=1 || [:tostr [:pick $files 0]]!=$acID || $acPath=$stablePath) do={ :error "M7ACCEPT owned directory or archive changed" }
:local p [$norm $acPath]
:foreach id in=[/container/find] do={
:local ref [$norm [/container/get $id root-dir]]
:if ([:pick $ref 0 [:len $p]]=$p || [:pick $p 0 [:len $ref]]=$ref) do={ :error "M7ACCEPT foreign container references storage" }
}
:foreach id in=[/container/mounts/find] do={
:local ref [$norm [/container/mounts/get $id src]]
:if ([:pick $ref 0 [:len $p]]=$p || [:pick $p 0 [:len $ref]]=$ref) do={ :error "M7ACCEPT foreign mount references storage" }
}
/file/remove $files
}
}
:if ([/file/get $stableFile contents]!=$deviceID) do={ :error "M7ACCEPT permanent router identity changed" }
$releaseLock $lockID $m7InstallTag $lockName
:if ([:len [/file/find where name=$lockName]]!=0) do={ :error "M7ACCEPT installation lock remains" }
:put "M7ACCEPT verified_scope=isolated-lan physical-lan=unverified rollback=verified cleanup=complete identity=preserved"
} do={
:put ("M7INSTALL interrupted: " . $failure)
:if ([:typeof $acAuthReceipt]="id") do={
:local copies [/file/find where name=($flash . "acd" . $acNonce . "/preview.auth")]
:if ([:len $copies]=1 && [:tostr [:pick $copies 0]]=[:tostr $acAuthReceipt]) do={
:local original [/file/find where name=($stateDir . "/ui-identity/preview.auth")]
:if ([:len $original]=1 && [/file/get $acAuthReceipt contents]=[/file/get $original contents]) do={ /file/remove $acAuthReceipt }
}
}
:local held [/system/scheduler/find where name=("m7ctl" . $suffix) and comment=($m7InstallOwner . "-policy-controller")]
:if ([:len $held]=1) do={ /system/scheduler/disable $held }
:local testPort [/interface/bridge/port/find where bridge=$m7InstallLan and interface=$acVeth and comment=$acOwner]
:if ([:len $testPort]=1) do={ /interface/bridge/port/disable $testPort }
:local testProc [/container/find where name=$acContainer and comment=$acOwner]
:if ([:len $testProc]=1) do={ :onerror e in={ /container/stop $testProc } do={} }
:put ("M7ACCEPT incomplete retained=" . $m7InstallName . " probe=" . $acContainer)
:foreach part in={"ac-in-drop";"ac-out-drop";"ac-wan-in";"ac-https";"ac-dns";"ac-dns-tcp"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident]="id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows]!=1 || (($rows->0)->"comment")!=$label || [$m7NetworkFingerprint ($rows->0)]!=($m7NetworkIDs->($label . "-sha"))) do={ :error "M7ACCEPT foreign rule; isolated resources retained" }
/ip/firewall/filter/remove $ident
}
}
:foreach part in={"ac-wan-nat"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident]="id") do={
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows]!=1 || (($rows->0)->"comment")!=$label || [$m7NetworkFingerprint ($rows->0)]!=($m7NetworkIDs->($label . "-sha"))) do={ :error "M7ACCEPT foreign rule; isolated resources retained" }
/ip/firewall/nat/remove $ident
}
}
:if ([:typeof $lockID] != "id") do={ :error "Meridian installer lock unavailable" }
:local attached [/interface/bridge/port/find where interface=$m7InstallVeth and bridge=$m7InstallBridge and comment=$m7InstallOwner]
:if ([:len $attached] = 1) do={ /interface/bridge/port/disable $attached }
:foreach objectName in=($gateName,$m7InstallName) do={
:local current [/container/find where name=$objectName and comment=$m7InstallOwner]
:if ([:len $current] = 1) do={ :onerror e in={ /container/set $current start-on-boot=no; /container/stop $current } do={} }
}
{
:local fingerprint do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local hostState ($m7ModeIDs->"host-state")
:if ([:typeof $hostState]="id") do={
:if ([/file/get $hostState contents]!=($m7ModeIDs->"initial")) do={ :error "Active host policy requires reversal before cleanup" }
}
:local worker ("m7ctl" . $suffix)
:local schedulerID ($m7ModeIDs->"scheduler")
:if ([:typeof $schedulerID]="id") do={
:if ([/system/scheduler/get $schedulerID comment]!=($m7InstallOwner . "-policy-controller") || [/system/scheduler/get $schedulerID name]!=$worker || [:tostr [/system/scheduler/get $schedulerID on-event]]!=$worker) do={ :error "Scheduler ownership changed" }
/system/scheduler/remove $schedulerID
}
:local workerID ($m7ModeIDs->"script")
:if ([:typeof $workerID]="id") do={
:if ([:len [/system/script/job/find where script=$worker]]>0 || [/system/script/get $workerID comment]!=($m7InstallOwner . "-policy-controller") || [/system/script/get $workerID name]!=$worker) do={ :error "Policy worker is busy or changed" }
/system/script/remove $workerID
}
:local hostConfig ($m7ModeIDs->"host-layout")
:if ([:typeof $hostConfig]="id") do={
:if ([:convert [/file/get $hostConfig contents] from=raw transform=sha512 to=hex]!=($m7ModeIDs->"digest")) do={ :error "Host policy layout changed" }
/file/remove $hostConfig
}
:if ([:typeof $hostState]="id") do={
/file/remove $hostState
}
:local f ($m7ModeIDs->"layout")
:if ([:typeof $f] = "id") do={
:local files [/file/find where name=($stateDir . "/policy-layout.json")]
:if ([:len $files] != 1 || [:tostr [:pick $files 0]] != [:tostr $f]) do={ :error "Policy layout ownership changed" }
:if ([:convert [/file/get $f contents] from=raw transform=sha512 to=hex] != ($m7ModeIDs->"digest")) do={ :error "Policy layout changed" }
/file/remove $f
}
:foreach role in={"v6-to";"v6-from"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/filter/remove $id
}
}
:foreach role in={"in6";"out6"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/raw/remove $id
}
}
:foreach role in={"in4";"out4";"router-in";"panel-in";"panel-out"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/raw/remove $id
}
}
:foreach role in={"gentry";"gd-white";"gl-white";"gl-black";"gk-white";"gk-black";"gr-white";"gr-black";"panel-reject";"vpn-in";"vpn-out"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/filter/remove $id
}
}
:foreach role in={"entry";"reply-white";"reply-black";"reply-new";"default-white";"listed-white";"listed-black";"keep-white";"keep-black";"local-white";"local-black"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/mangle/remove $id
}
}
:foreach role,id in=$m7ModeIDs do={
:if ([:pick $role 0 10]="connected-" && [:typeof $id]="id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows]!=1 || [$fingerprint ($rows->0)]!=($m7ModeIDs->("sha-" . $role))) do={ :error "Connected network ownership changed" }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"link-keep";"lan-keep"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"via";"kill"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/route/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/route/remove $id
}
}
:foreach role in={"table"} do={
:local id ($m7ModeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/routing/table/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($m7ModeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/routing/table/remove $id
}
}
}
:foreach part in={"panel-dnat";"wan-nat"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/nat/remove $ident
}
}
:foreach part in={"panel-access";"panel-reply";"wan-out";"wan-return";"drop-router";"drop-input";"drop-output"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/filter/remove $ident
}
}
:foreach part in={"drop6-to";"drop6-from";"drop6-router"} do={
:local label ($m7InstallOwner . "-" . $part)
:local ident ($m7NetworkIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$m7NetworkFingerprint ($rows->0)] != ($m7NetworkIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ipv6/firewall/filter/remove $ident
}
}
:local ownedIP [/ip/address/find where interface=$m7InstallBridge and comment=$m7InstallOwner and address=([:tostr $m7InstallGateway] . "/30")]
:if ([:len $ownedIP] = 1) do={ /ip/address/remove $ownedIP }
:if ([:len $attached] = 1 && [/interface/bridge/port/get $attached comment] = $m7InstallOwner && [/interface/bridge/port/get $attached bridge] = $m7InstallBridge && [/interface/bridge/port/get $attached interface] = $m7InstallVeth) do={ /interface/bridge/port/remove $attached }
:local createdBridge [/interface/bridge/find where name=$m7InstallBridge and comment=$m7InstallOwner]
:if ([:len $createdBridge] = 1 && [:len [/interface/bridge/port/find where bridge=$m7InstallBridge]] = 0 && [:len [/ip/address/find where interface=$m7InstallBridge]] = 0 && [:len [/ipv6/address/find where interface=$m7InstallBridge and dynamic=no]] = 0) do={ /interface/bridge/remove $createdBridge }
:local knownGate ($m7GateState->"gateID")
:if ([:typeof $knownGate] = "id" && ($m7GateState->"retired") != true && [:len [/file/find where name=($stateDir . "/key") or name=($stateDir . "/ui-identity")]] = 0) do={
:if (![$m7RetireGate $m7GateState]) do={ :error "Verifier recovery required" }
:local oldMount [/container/mounts/find where list=$m7InstallMount and dst="/state"]
:local oldLink [/interface/veth/find where name=$m7InstallVeth and comment=$m7InstallOwner]
:if ([:len $oldMount] != 1 || [:tostr [:pick $oldMount 0]] != [:tostr ($m7GateState->"mountID")] || [:len $oldLink] != 1 || [:tostr [:pick $oldLink 0]] != [:tostr ($m7GateState->"vethID")] || [:len [/container/find where interface=$m7InstallVeth]] > 0) do={ :error "Verifier mount or VETH ownership changed" }
:foreach other in=[/container/find] do={
:local lists ("," . [:tostr [/container/get $other mountlists]] . ",")
:if ([:typeof [:find $lists ("," . $m7InstallMount . ",")]] = "num") do={ :error "Verifier mount still in use" }
}
/container/mounts/remove $oldMount
/interface/veth/remove $oldLink
:local clean true
:local prefix ($stateDir . "/")
:foreach member in=[/file/find] do={
:local pathName [/file/get $member name]
:if ([:pick $pathName 0 [:len $prefix]] = $prefix && $pathName != ($prefix . "bootstrap-ready") && $pathName != ($prefix . "device-id")) do={ :set clean false }
}
:local saved [/file/find where name=($prefix . "device-id")]
:if ($clean && [:len $saved] = 1 && [/file/get $saved contents] = ($deviceID . "\n")) do={
:local sd [/file/find where name=$stateDir]
:if ([:len $sd] = 1 && [/file/get $sd type] = "directory") do={ /file/remove $sd }
}
$releaseLock $lockID $m7InstallTag $lockName
:put "M7INSTALL verifier removed; network restored; retry is available"
:error "Meridian installation incomplete"
}
$releaseLock $lockID $m7InstallTag $lockName
:put ("M7INSTALL network rules reverted; retained installation " . $m7InstallName . " gate " . $gateName . " state " . $stateDir . " archive " . $m7ImagePath)
:error "Meridian installation incomplete"
}
}
