#!/usr/bin/env bash

## SKYWIRE

tmux new -s skywire -d

source ./integration/generic/env-vars.sh

echo "Checking transport-discovery is up"
curl --retry 5  --retry-connrefused 1 --connect-timeout 5 https://transport.discovery.skywire.skycoin.com/security/nonces/$PK_A

tmux rename-window -t skywire NodeA
tmux send-keys -t NodeA -l "./skywire-visor ./integration/generic/nodeA.json --tag NodeA $SYSLOG_OPTS"
tmux send-keys C-m
tmux new-window -t skywire -n NodeB
tmux send-keys -t NodeB -l "./skywire-visor ./integration/intermediary-nodeB.json --tag NodeB $SYSLOG_OPTS"
tmux send-keys C-m
tmux new-window -t skywire -n NodeC
tmux send-keys -t NodeC -l "./skywire-visor ./integration/generic/nodeC.json --tag NodeC $SYSLOG_OPTS"
tmux send-keys C-m

tmux new-window -t skywire -n shell

tmux send-keys -t shell 'source ./integration/generic/env-vars.sh' C-m

tmux attach -t skywire
