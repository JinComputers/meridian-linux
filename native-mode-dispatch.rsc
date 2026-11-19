{
:local m7Controller do={
:local m7Self [:jobname]
:if ([:len [/system/script/job/find where script=$m7Self]] > 1) do={ :return 0 }
:local m7Ctl [/system/script/find where name=$m7Self]
:if ([:len $m7Ctl]!=1) do={ :error "Policy controller ownership" }
:local m7Comment [:tostr [/system/script/get [:pick $m7Ctl 0] comment]]
:local m7Nonce [:pick $m7Comment 17 49]
:if (!($m7Nonce~"^[0-9a-f]+\$") || [:len $m7Nonce]!=32 || $m7Comment!=("meridian-install:" . $m7Nonce . "-policy-controller")) do={ :error "Policy owner mismatch" }
:local m7Short [:pick $m7Nonce 0 8]
:local m7Disk ""
:if ([:len [/file/find where name="flash" and type="disk"]]=1) do={ :set m7Disk "flash/" }
:local m7Base ($m7Disk . "m7s" . $m7Short . "/")
:local m7Input ($m7Base . "policy-request.json")
:local m7Status ($m7Base . "policy-host-status.json")
:local m7StateName ($m7Disk . "m7x" . $m7Short . ".json")
:local m7Read do={
    :local fn $1
    :local ids [/file/find where name=$fn]
    :if ([:len $ids]!=1) do={ :error "Policy file missing" }
    :local fid [:pick $ids 0]
    :local size [/file/get $fid size]
    :if ($size<2 || $size>1048576) do={ :error "Policy file size" }
    :local body ""
    :local offset 0
    :while ($offset<$size) do={
        :local want ($size-$offset)
        :if ($want>32768) do={ :set want 32768 }
        :local part ([/file/read file=$fn offset=$offset chunk-size=$want as-value]->"data")
        :if ([:len $part]!=$want) do={ :error "Policy file incomplete" }
        :set body ($body . $part)
        :set offset ($offset + $want)
    }
    :if ([:tostr [:pick [/file/find where name=$fn] 0]]!=[:tostr $fid] || [/file/get $fid size]!=$size) do={ :error "Policy file changed" }
    :return $body
}
:local m7Write do={
    :local fn $1
    :local body [:serialize to=json value=$2 options=json.no-string-conversion]
    :local ids [/file/find where name=$fn]
    :if ([:len $ids]=0) do={ /file/add name=$fn type=file contents=$body } else={
        :if ([:len $ids]!=1) do={ :error "Ambiguous policy file" }
        :if ([/file/get [:pick $ids 0] contents]!=$body) do={ /file/set [:pick $ids 0] contents=$body }
    }
    :if ([/file/get [:pick [/file/find where name=$fn] 0] contents]!=$body) do={ :error "Policy state readback" }
}
:local m7Meta [:deserialize from=json value=[$m7Read ($m7Disk . "m7h" . $m7Short . ".json")] options=json.no-string-conversion]
:local m7State [:deserialize from=json value=[$m7Read $m7StateName] options=json.no-string-conversion]
:if (($m7Meta->"tag")!=$m7Nonce || ($m7Meta->"mode_tag")!=$m7Short || ($m7Meta->"version")!=1) do={ :error "Policy layout mismatch" }
:local m7Publish do={
    :local status [:toarray ""]
    :set ($status->"schema") 1
    :set ($status->"owner") $2
    :set ($status->"request_id") ($3->"request_id")
    :set ($status->"mode_revision") ($3->"mode_revision")
    :set ($status->"mode") ($3->"mode")
    :set ($status->"status") $4
    :local writer $5
    $writer $1 $status
}
:local m7Close do={
    :local meta $1
    :local r ($meta->"objects")
    :local own ("meridian-install:" . ($meta->"tag") . "-policy-")
    :local mark ("m7-" . ($meta->"mode_tag") . "-")
    :foreach role in={"panel-reject";"v6-from";"v6-to";"panel-out";"panel-in";"router-in";"out4";"in4";"out6";"in6"} do={
        :local ident ($r->$role)
        :if ([:len [:tostr $ident]]>0) do={
            :local jid [:toid $ident]
            :if ($role="panel-reject") do={
                :if ([/ip/firewall/filter/get $jid comment]!=($own . $role)) do={ :error "Panel filter drift" }
                /ip/firewall/filter/enable $jid
            } else={
                :if ($role="v6-from" || $role="v6-to") do={
                    :if ([/ipv6/firewall/filter/get $jid comment]!=($own . $role) || [/ipv6/firewall/filter/get $jid action]!="drop") do={ :error "IPv6 forward drift" }
                    /ipv6/firewall/filter/enable $jid
                } else={
                    :if ($role="out6" || $role="in6") do={
                        :if ([/ipv6/firewall/raw/get $jid comment]!=($mark . $role) || [/ipv6/firewall/raw/get $jid action]!="drop") do={ :error "IPv6 raw drift" }
                        /ipv6/firewall/raw/enable $jid
                    } else={
                        :local label ($own . $role)
                        :if ($role="out4" || $role="in4") do={ :set label ($mark . $role) }
                        :if ([/ip/firewall/raw/get $jid comment]!=$label) do={ :error "IPv4 raw drift" }
                        /ip/firewall/raw/enable $jid
                    }
                }
            }
        }
    }
    :local ready false
    :for i from=1 to=40 do={
        :set ready true
        :foreach role in={"out4";"in4"} do={ :local jid [:toid ($r->$role)]; :if ([/ip/firewall/raw/get $jid invalid] || [/ip/firewall/raw/get $jid disabled]) do={ :set ready false } }
        :foreach role in={"out6";"in6"} do={ :if ([:len [:tostr ($r->$role)]]>0) do={ :local jid [:toid ($r->$role)]; :if ([/ipv6/firewall/raw/get $jid invalid] || [/ipv6/firewall/raw/get $jid disabled]) do={ :set ready false } } }
        :foreach role in={"v6-from";"v6-to"} do={ :local jid [:toid ($r->$role)]; :if ([/ipv6/firewall/filter/get $jid invalid] || [/ipv6/firewall/filter/get $jid disabled]) do={ :set ready false } }
        :if ($ready) do={ :break }
        :delay 250ms
    }
    :if (!$ready) do={ :error "Quarantine not ready" }
}
:local m7Profile do={
    :local meta $1
    :local wanted $2
    :local dstSet $3
    :local r ($meta->"objects")
    :local short ($meta->"mode_tag")
    :local mark ("m7-" . $short . "-")
    :local own ("meridian-install:" . ($meta->"tag") . "-policy-")
    :local entry [:toid ($r->"entry")]
    :local gate [:toid ($r->"gentry")]
    :if ([/ip/firewall/mangle/get $entry comment]!=($mark . "entry") || [/ip/firewall/filter/get $gate comment]!=($mark . "gentry") || [:tostr [/ip/firewall/mangle/get $entry src-address]]!=($meta->"source") || [/ip/firewall/mangle/get $entry in-interface]!=($meta->"lan")) do={ :error "Policy selector drift" }
    :foreach role in={"vpn-out";"vpn-in"} do={
        :local jid [:toid ($r->$role)]
        :if ([/ip/firewall/filter/get $jid comment]!=($own . $role)) do={ :error "Policy permit drift" }
    }
    :local inbound [:toid ($r->"reply-new")]
    :if ([/ip/firewall/mangle/get $inbound comment]!=($mark . "reply-new")) do={ :error "Reply marker drift" }
    :if ($wanted="off") do={
        /ip/firewall/mangle/disable $entry
        /ip/firewall/filter/disable $gate
        /ip/firewall/mangle/disable $inbound
        :foreach role in={"vpn-in";"vpn-out"} do={ /ip/firewall/filter/disable [:toid ($r->$role)] }
        :return 0
    }
    :if (($wanted!="black" && $wanted!="white") || !($dstSet~"^m7g[0-9a-f]+\$")) do={ :error "Invalid prepared route destination" }
    :local chip "b"
    :if ($wanted="white") do={ :set chip "w" }
    :local bodyName ("m7" . $chip . $short)
    :local guardName ("m7g" . $chip . $short)
    :local body [/ip/firewall/mangle/find where chain=$bodyName]
    :local localRule [:toid ($r->("local-" . $wanted))]
    :if ([:tostr [:pick $body 0]]!=[:tostr $localRule] || [/ip/firewall/mangle/get $localRule action]!="accept" || [:tostr [/ip/firewall/mangle/get $localRule dst-address-type]]!="local" || [/ip/firewall/mangle/get $localRule disabled]) do={ :error "Panel local bypass changed" }
    :local matchRule [:toid ($r->("listed-" . $wanted))]
    :local guardRule [:toid ($r->("gl-" . $wanted))]
    :if ([/ip/firewall/mangle/get $matchRule comment]!=($mark . "listed-" . $wanted) || [/ip/firewall/filter/get $guardRule comment]!=($mark . "gl-" . $wanted)) do={ :error "List ownership mismatch" }
    /ip/firewall/mangle/set $matchRule dst-address-list=$dstSet
    /ip/firewall/filter/set $guardRule dst-address-list=$dstSet
    :foreach role in={"vpn-out";"vpn-in"} do={ /ip/firewall/filter/enable [:toid ($r->$role)] }
    /ip/firewall/mangle/enable $inbound
    /ip/firewall/filter/set $gate jump-target=$guardName disabled=no
    /ip/firewall/mangle/set $entry jump-target=$bodyName disabled=no
    :if ([/ip/firewall/filter/get $gate jump-target]!=$guardName || [/ip/firewall/filter/get $gate disabled] || [/ip/firewall/mangle/get $entry jump-target]!=$bodyName || [/ip/firewall/mangle/get $entry disabled] || [:tostr [/ip/firewall/mangle/get $matchRule dst-address-list]]!=$dstSet) do={ :error "Mode readback failed" }
}
:local m7Open do={
    :local r ($1->"objects")
    :local own ("meridian-install:" . ($1->"tag") . "-policy-")
    :local mr ("m7-" . ($1->"mode_tag") . "-")
    :delay 1s
    :foreach role in={"in4";"out4"} do={ :local jid [:toid ($r->$role)]; :if ([/ip/firewall/raw/get $jid comment]!=($mr . $role)) do={ :error "IPv4 quarantine owner" }; /ip/firewall/raw/disable $jid }
    :foreach role in={"in6";"out6"} do={ :if ([:len [:tostr ($r->$role)]]>0) do={ :local jid [:toid ($r->$role)]; :if ([/ipv6/firewall/raw/get $jid comment]!=($mr . $role)) do={ :error "IPv6 quarantine owner" }; /ipv6/firewall/raw/disable $jid } }
    :local settled false
    :for attempt from=1 to=12 do={
        :delay 250ms
        :set settled true
        :foreach role in={"in4";"out4"} do={ :local jid [:toid ($r->$role)]; :if (![/ip/firewall/raw/get $jid disabled]) do={ :set settled false } }
        :foreach role in={"in6";"out6"} do={ :if ([:len [:tostr ($r->$role)]]>0) do={ :local jid [:toid ($r->$role)]; :if (![/ipv6/firewall/raw/get $jid disabled]) do={ :set settled false } } }
        :if ($settled && $attempt>=4) do={ :break }
    }
    :if (!$settled) do={ :error "Quarantine release unconfirmed" }
    :if ($2="off") do={
        :foreach role in={"v6-from";"v6-to"} do={ :local jid [:toid ($r->$role)]; :if ([/ipv6/firewall/filter/get $jid comment]!=($own . $role)) do={ :error "IPv6 guard owner" }; /ipv6/firewall/filter/disable $jid }
    }
}
:local m7DNS do={
    :local generation $1
    :local label $2
    :if ([:len $generation]=0) do={ :return 0 }
    :foreach rid in=[/ip/dns/static/find where comment=$label] do={
        :if ([:tostr [/ip/dns/static/get $rid address-list]]!=$generation || [:tostr [/ip/dns/static/get $rid type]]!="FWD") do={ :error "DNS generation drift" }
        :if ([:tostr $3]="true") do={ /ip/dns/static/enable $rid } else={ /ip/dns/static/disable $rid }
    }
}
:local m7Remove do={
    :local gen $1
    :local label $2
    :if ([:len $gen]=0) do={ :return 0 }
    :foreach rid in=[/ip/dns/static/find where comment=$label] do={ :if ([:tostr [/ip/dns/static/get $rid address-list]]!=$gen || [:tostr [/ip/dns/static/get $rid type]]!="FWD") do={ :error "Foreign DNS record" }; /ip/dns/static/remove $rid }
    :foreach rid in=[/ip/firewall/address-list/find where comment=$label] do={ :if ([:tostr [/ip/firewall/address-list/get $rid list]]!=$gen) do={ :error "Foreign address record" }; :if (![/ip/firewall/address-list/get $rid dynamic]) do={ /ip/firewall/address-list/remove $rid } }
}
:local m7Label ("meridian-install:" . $m7Nonce . "-generation-")
:local m7OldMode ($m7State->"mode")
:local m7OldList ($m7State->"list")
:if ($m7OldMode!="off" && $m7OldMode!="black" && $m7OldMode!="white") do={ :error "Host policy state invalid" }
:local m7Phase ($m7State->"phase")
:local m7Proof do={
    :local r ($1->"objects")
    :local a [/ip/firewall/filter/get [:toid ($r->"vpn-out")] packets]
    :local b [/ip/firewall/filter/get [:toid ($r->"vpn-in")] packets]
    :local out [:toarray ""]
    :set ($out->"out") [:tonum $a]
     :set ($out->"in") [:tonum $b]
    :local held 0
    :foreach role in={"in4";"out4"} do={ :set held ($held+[:tonum [/ip/firewall/raw/get [:toid ($r->$role)] packets]]) }
    :set ($out->"held") $held
    :return $out
}
:local m7Pending ($m7State->"next")
:if ($m7Phase!="stable") do={
    :if ([:typeof $m7Pending]!="array") do={ :error "Pending policy state unavailable" }
    :local now [:tonsec [/system/resource/get uptime]]
    :if ($m7Phase="waiting" && $now>=($m7State->"started")) do={
        :local live [/container/print as-value where comment=("meridian-install:" . $m7Nonce)]
        :local current [$m7Read $m7Input]
        :local same ([:convert $current from=raw transform=sha512 to=hex]=($m7State->"wire"))
        :if ([:len $live]=1 && [:tostr (($live->0)->"running")]="true" && $same) do={
            :local c [$m7Proof $m7Meta]
            :if (($c->"held")>($m7State->"held")) do={
                :set ($m7State->"held") ($c->"held")
                :set ($m7State->"out") ($c->"out")
                :set ($m7State->"in") ($c->"in")
                :set ($m7State->"started") $now
                $m7Write $m7StateName $m7State
                :return 0
            }
            :local proof (($c->"out")>($m7State->"out") && ($c->"in")>($m7State->"in"))
            :if (!$proof && ($now-($m7State->"started"))<10000000000 && ($m7State->"empty")!=true) do={ :return 0 }
            :local disposition "configured"
            :if ($proof) do={ :set disposition "applied" }
            :set ($m7State->"mode") ($m7Pending->"mode")
            :set ($m7State->"list") ($m7State->"next_list")
            :set ($m7State->"digest") ($m7Pending->"request_id")
            :set ($m7State->"last") ($m7Pending->"request_id")
            :set ($m7State->"phase") "stable"
            :set ($m7State->"verification") $disposition
            $m7Write $m7StateName $m7State
            $m7Publish $m7Status $m7Nonce $m7Pending $disposition $m7Write
            :if ([:len $m7OldList]>0 && $m7OldList!=($m7State->"list")) do={ $m7Remove $m7OldList ($m7Label . $m7OldList) }
            :return 0
        }
    }
    :onerror failure in={
        $m7Close $m7Meta
        $m7DNS ($m7State->"next_list") ($m7Label . ($m7State->"next_list")) false
        $m7DNS $m7OldList ($m7Label . $m7OldList) true
        $m7Profile $m7Meta $m7OldMode $m7OldList
        $m7Open $m7Meta $m7OldMode
        :if (($m7State->"next_list")!=$m7OldList) do={ $m7Remove ($m7State->"next_list") ($m7Label . ($m7State->"next_list")) }
        :set ($m7State->"phase") "stable"
        :set ($m7State->"last") ($m7Pending->"request_id")
        $m7Write $m7StateName $m7State
        $m7Publish $m7Status $m7Nonce $m7Pending "rolled-back" $m7Write
    } do={ :log warning ("M7POLICY rollback: " . $failure); $m7Publish $m7Status $m7Nonce $m7Pending "recovery" $m7Write }
    :return 0
}
:if ([:len [/file/find where name=$m7Input]]=0) do={ :return 0 }
:local m7Wire [$m7Read $m7Input]
:local m7Req [:deserialize from=json value=$m7Wire options=json.no-string-conversion]
:local m7ID ($m7Req->"request_id")
:local m7Mode ($m7Req->"mode")
:if (($m7Req->"schema")!=1 || ($m7Req->"owner")!=$m7Nonce) do={ :error "Invalid panel owner" }
:if (($m7Req->"status")="unavailable") do={ :return 0 }
:if ([:len $m7ID]!=64 || !($m7ID~"^[0-9a-f]+\$") || [:typeof ($m7Req->"mode_revision")]!="num") do={ :error "Invalid panel request" }
 :local signature ("\"request_id\":\"" . $m7ID . "\"")
 :local cursor [:find $m7Wire $signature]
 :if ([:typeof $cursor]!="num" || [:typeof [:find $m7Wire $signature ($cursor+1)]]="num") do={ :error "Invalid policy digest field" }
 :local canonical ([:pick $m7Wire 0 $cursor] . "\"request_id\":\"\"" . [:pick $m7Wire ($cursor+[:len $signature]) [:len $m7Wire]])
 :if ([:pick [:convert $canonical from=raw transform=sha512 to=hex] 0 64]!=$m7ID) do={ :error "Panel request digest mismatch" }
 :if ($m7ID=($m7State->"last")) do={
    :if (($m7State->"digest")=$m7ID && ($m7State->"verification")="configured") do={
        :local c [$m7Proof $m7Meta]
        :if (($c->"held")>($m7State->"held")) do={
            :set ($m7State->"held") ($c->"held")
            :set ($m7State->"out") ($c->"out")
            :set ($m7State->"in") ($c->"in")
            $m7Write $m7StateName $m7State
            :return 0
        }
        :if (($c->"out")>($m7State->"out") && ($c->"in")>($m7State->"in")) do={
            :set ($m7State->"verification") "applied"
            $m7Write $m7StateName $m7State
            $m7Publish $m7Status $m7Nonce $m7Req "applied" $m7Write
        }
    }
    :return 0
}
:if (($m7Req->"status")!="requested" || ($m7Mode!="black" && $m7Mode!="white")) do={ :if ($m7Mode="black" || $m7Mode="white") do={ $m7Publish $m7Status $m7Nonce $m7Req "unavailable" $m7Write }; :return 0 }
 :foreach k in={"schema";"owner";"status";"mode";"mode_revision";"request_id";"prefixes";"domains";"wildcards"} do={
    :local needle ("\"" . $k . "\":")
    :local pos [:find $m7Wire $needle]
    :if ([:typeof $pos]!="num" || [:typeof [:find $m7Wire $needle ($pos+1)]]="num") do={ :error "Duplicate or missing policy field" }
}
:local m7Request [:toarray ""]
:set ($m7Request->"request_id") $m7ID
:set ($m7Request->"mode_revision") ($m7Req->"mode_revision")
:set ($m7Request->"mode") $m7Mode
:local m7Gen ("m7g" . $m7Short . [:rndstr from="0123456789abcdef" length=24])
:local m7Own ($m7Label . $m7Gen)
:local m7Rejected false
:onerror preflight in={
    :if ([:len $m7Req]!=9 || [:typeof ($m7Req->"prefixes")]!="array" || [:typeof ($m7Req->"domains")]!="array" || [:typeof ($m7Req->"wildcards")]!="array" || ([:len ($m7Req->"prefixes")]+[:len ($m7Req->"domains")]+[:len ($m7Req->"wildcards")])>4096) do={ :error "Panel request shape" }
    :local live [/container/print as-value where comment=("meridian-install:" . $m7Nonce)]
    :if ([:len $live]!=1 || [:tostr (($live->0)->"running")]!="true") do={ :error "VPN stack offline" }
    :foreach rid in=[/ip/firewall/filter/find where action=fasttrack-connection] do={ :if (![/ip/firewall/filter/get $rid disabled]) do={ :error ("/ip/firewall/filter " . [:tostr $rid] . " FastTrack overlaps policy") } }
    :if ([:len [/ip/firewall/connection/find where fasttrack=yes]]>0) do={ :error "Active FastTrack connections overlap policy" }
    :foreach rid in=[/routing/rule/find] do={ :if (![/routing/rule/get $rid disabled]) do={ :error ("/routing/rule " . [:tostr $rid] . " overlaps policy") } }
    :foreach rid in=[/ip/firewall/mangle/find] do={
        :local cm [:tostr [/ip/firewall/mangle/get $rid comment]]
        :if (![/ip/firewall/mangle/get $rid disabled] && [:pick $cm 0 12]!=("m7-" . $m7Short . "-")) do={
            :local m7Action [/ip/firewall/mangle/get $rid action]
            :local m7Chain [/ip/firewall/mangle/get $rid chain]
            :local harmless ($m7Action="passthrough" || ($m7Action="change-mss" && $m7Chain="forward"))
            :if (!$harmless && ($m7Chain="prerouting" || $m7Chain="forward" || $m7Chain="input")) do={
                :local m7Iface [:tostr [/ip/firewall/mangle/get $rid in-interface]]
                :if ([:len $m7Iface]>0 && [:pick $m7Iface 0 1]!="!" && $m7Iface!=($m7Meta->"lan") && $m7Iface!=($m7Meta->"wan") && $m7Iface!=($m7Meta->"link") && $m7Iface!=($m7Meta->"veth") && [:len [/interface/find where name=$m7Iface]]=1) do={ :set harmless true }
            }
            :if (!$harmless && $m7Chain="output") do={
                :local m7Iface [:tostr [/ip/firewall/mangle/get $rid out-interface]]
                :if ([:len $m7Iface]>0 && [:pick $m7Iface 0 1]!="!" && $m7Iface!=($m7Meta->"lan") && $m7Iface!=($m7Meta->"wan") && $m7Iface!=($m7Meta->"link") && $m7Iface!=($m7Meta->"veth") && [:len [/interface/find where name=$m7Iface]]=1) do={ :set harmless true }
            }
            :if (!$harmless) do={ :error ("/ip/firewall/mangle " . [:tostr $rid] . " chain=" . $m7Chain . " action=" . $m7Action . " overlaps policy") }
        }
    }
    :foreach rid in=[/ip/firewall/raw/find where disabled=no] do={ :local cm [:tostr [/ip/firewall/raw/get $rid comment]]; :if ([:pick $cm 0 12]!=("m7-" . $m7Short . "-") && [:pick $cm 0 49]!=("meridian-install:" . $m7Nonce)) do={ :error "Foreign IPv4 raw rule" } }
    :foreach rid in=[/ipv6/firewall/raw/find where disabled=no] do={ :local cm [:tostr [/ipv6/firewall/raw/get $rid comment]]; :if ([:pick $cm 0 12]!=("m7-" . $m7Short . "-")) do={ :error "Foreign IPv6 raw rule" } }
    :if ([:len [/ip/firewall/address-list/find where list=$m7Gen]]>0 || [:len [/ip/dns/static/find where comment=$m7Own]]>0) do={ :error "Generation name collision" }
    :foreach p in=($m7Req->"prefixes") do={
        :local slash [:find $p "/"]
        :if ([:typeof $p]!="str" || [:typeof $slash]!="num") do={ :error "Invalid prefix" }
        :local ip [:toip [:pick $p 0 $slash]]
        :local bits [:tonum [:pick $p ($slash+1) [:len $p]]]
        :if ([:typeof $ip]!="ip" || $ip=0.0.0.0 || ($ip in 224.0.0.0/4) || [:typeof $bits]!="num" || $bits<1 || $bits>32) do={ :error "Invalid IPv4 selector" }
    }
    :if ([:len ($m7Req->"domains")]+[:len ($m7Req->"wildcards")]>0) do={
        :local lanNet ($m7Meta->"source")
        :local dhcp [/ip/dhcp-server/network/find where address=$lanNet]
        :if (![/ip/dns/get allow-remote-requests] || [:len $dhcp]!=1 || [:tostr [/ip/dhcp-server/network/get [:pick $dhcp 0] dns-server]]!=($m7Meta->"router")) do={ :error "Router LAN DNS path unverified" }
    }
    :foreach field in={"domains";"wildcards"} do={
        :foreach d in=($m7Req->$field) do={ :if ([:typeof $d]!="str" || [:len $d]>253 || !($d~"^[a-z0-9-]+([.][a-z0-9-]+)*\$")) do={ :error "Invalid DNS selector" } }
    }
} do={ :log warning ("M7POLICY preflight: " . $preflight); :set m7Rejected true }
:if ($m7Rejected) do={ $m7Publish $m7Status $m7Nonce $m7Request "unavailable" $m7Write; :return 0 }
:set ($m7State->"phase") "preparing"
:set ($m7State->"next") $m7Request
:set ($m7State->"next_list") $m7Gen
:set ($m7State->"wire") [:convert $m7Wire from=raw transform=sha512 to=hex]
$m7Write $m7StateName $m7State
$m7Publish $m7Status $m7Nonce $m7Request "pending" $m7Write
:onerror failure in={
    :foreach p in=($m7Req->"prefixes") do={
        :local created [/ip/firewall/address-list/add list=$m7Gen address=$p comment=$m7Own]
        :local observed [:tostr [/ip/firewall/address-list/get $created address]]
        :if ($observed!=$p && ($observed . "/32")!=$p) do={ :error "Prefix normalized to different range" }
    }
    :foreach d in=($m7Req->"domains") do={
        /ip/firewall/address-list/add list=$m7Gen address=$d comment=$m7Own
        /ip/dns/static/add type=FWD name=$d match-subdomain=yes address-list=$m7Gen comment=$m7Own disabled=yes
    }
    :foreach d in=($m7Req->"wildcards") do={
        :local rx "^([a-z0-9-]+[.])+"
        :for i from=0 to=([:len $d]-1) do={ :local ch [:pick $d $i ($i+1)]; :if ($ch=".") do={ :set ch "[.]" }; :set rx ($rx . $ch) }
        :set rx ($rx . "\$")
        /ip/dns/static/add type=FWD regexp=$rx address-list=$m7Gen comment=$m7Own disabled=yes
    }
    :if ([$m7Read $m7Input]!=$m7Wire) do={ :error "Panel selection changed" }
    $m7Close $m7Meta
    $m7DNS $m7OldList ($m7Label . $m7OldList) false
    $m7DNS $m7Gen $m7Own true
    $m7Profile $m7Meta $m7Mode $m7Gen
    :local c [$m7Proof $m7Meta]
    :set ($m7State->"in") ($c->"in")
    :set ($m7State->"out") ($c->"out")
    :set ($m7State->"held") ($c->"held")
    :set ($m7State->"started") [:tonsec [/system/resource/get uptime]]
    :set ($m7State->"empty") ($m7Mode="black" && ([:len ($m7Req->"prefixes")]+[:len ($m7Req->"domains")]+[:len ($m7Req->"wildcards")])=0)
    :set ($m7State->"phase") "waiting"
    $m7Write $m7StateName $m7State
    $m7Open $m7Meta $m7Mode
} do={ :log warning ("M7POLICY activation: " . $failure); :set ($m7State->"phase") "recovery"; $m7Write $m7StateName $m7State; $m7Publish $m7Status $m7Nonce $m7Request "recovery" $m7Write }
}
:local m7Result [$m7Controller]
}
