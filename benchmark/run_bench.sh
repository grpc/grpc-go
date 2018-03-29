#!/bin/bash

rs=(1)
cs=(1)
w=10
d=10
reqs=(1)
resps=(1)
rpc_types=(unary)

# idx[0] = idx value for rs
# idx[1] = idx value for cs
# idx[2] = idx value for reqs
# idx[3] = idx value for resps
# idx[4] = idx value for rpc_types
idx=(0 0 0 0 0)
idx_max=(1 1 1 1 1)

inc()
{
  for i in $(seq $((${#idx[@]}-1)) -1 0)
  do
    idx[${i}]=$((${idx[${i}]}+1))
    if [ ${idx[${i}]} == ${idx_max[${i}]} ]; then
      idx[${i}]=0
    else
      break
    fi
  done
  return=1
  # Check to see if we have looped back to the beginning.
  for v in ${idx[@]}; do
    if [ ${v} == 0 ]; then
      return=$((${return}*1))
    else
      return=$((${return}*0))
    fi
  done
  if [ ${return} == 1 ]; then
  rm -Rf ${out_dir}
    exit 0
  fi
}

run(){
  nr=${rs[${idx[0]}]}
  nc=${cs[${idx[1]}]}
  req_sz=${reqs[${idx[2]}]}
  resp_sz=${resps[${idx[3]}]}
  r_type=${rpc_types[${idx[4]}]}
  # Following runs one benchmark
  base_port=50051
  delta=0
  test_name="r_"${nr}"_c_"${nc}"_req_"${req_sz}"_resp_"${resp_sz}"_"${r_type}"_"$(date +%s)
  echo "================================================================================"
  echo ${test_name}
  while :
  do
    port=$((${base_port}+${delta}))

    # Launch the server in background
    ${out_dir}/server --port=${port} --test_name="Server_"${test_name}&
    server_pid=$(echo $!)

    # Launch the client
    ${out_dir}/client --port=${port} --d=${d} --w=${w} --r=${nr} --c=${nc} --req=${req_sz} --resp=${resp_sz} --rpc_type=${r_type}  --test_name="client_"${test_name}
    client_status=$(echo $?)

    kill ${server_pid}
    wait ${server_pid}

    if [ ${client_status} == 0 ]; then
      break
    fi

    delta=$((${delta}+1))
    if [ ${delta} == 10 ]; then
      echo "Continuous 10 failed runs. Exiting now."
      rm -Rf ${out_dir}
      exit 1
    fi
  done

}

while test $# -gt 0; do
  case "$1" in
    -r)
      shift
      if test $# -gt 0; then
        rs=($(echo $1 | sed 's/,/ /g'))
        idx_max[0]=${#rs[@]}
      else
        echo "number of rpcs not specified"
        exit 1
      fi
      shift
      ;;
    -c)
      shift
      if test $# -gt 0; then
        cs=($(echo $1 | sed 's/,/ /g'))
        idx_max[1]=${#cs[@]}
      else
        echo "number of connections not specified"
        exit 1
      fi
      shift
      ;;
    -w)
      shift
      if test $# -gt 0; then
        w=$1
      else
        echo "warm-up period not specified"
        exit 1
      fi
      shift
      ;;
    -d)
      shift
      if test $# -gt 0; then
        d=$1
      else
        echo "duration not specified"
        exit 1
      fi
      shift
      ;;
    -req)
      shift
      if test $# -gt 0; then
        reqs=($(echo $1 | sed 's/,/ /g'))
        idx_max[2]=${#reqs[@]}
      else
        echo "request size not specified"
        exit 1
      fi
      shift
      ;;
    -resp)
      shift
      if test $# -gt 0; then
        resps=($(echo $1 | sed 's/,/ /g'))
        idx_max[3]=${#resps[@]}
      else
        echo "response size not specified"
        exit 1
      fi
      shift
      ;;
    -rpc_type)
      shift
      if test $# -gt 0; then
        rpc_types=($(echo $1 | sed 's/,/ /g'))
        idx_max[4]=${#rpc_types[@]}
        for val in ${rpc_types[@]}; do
          case "${val}" in
            "unary"|"streaming");;
            *) echo "Incorrect value for rpc_type"
              exit 1
              ;;
          esac

        done
      else
        echo "rpc type not specified not specified"
        exit 1
      fi
      shift
      ;;
    -h|--help)
      echo "Following are valid options:"
      echo ""
      echo "-h, --help        show brief help"
      echo "-w                warm-up duration in seconds, default value is 10"
      echo "-d                benchmark duration in seconds, default value is 60"
      echo ""
      echo "Each of the following can have multiple comma separated values."
      echo ""
      echo "-r                number of RPCs, default value is 1"
      echo "-c                number of Connections, default value is 1"
      echo "-req              req size in bytes, default value is 1"
      echo "-resp             resp size in bytes, default value is 1"
      echo "-rpc_type         valid values are unary|streaming, default is unary"
      ;;
    *)
      echo "Incorrect option"
      exit 1
      ;;
  esac
done

# Build server and client
out_dir=$(mktemp -d oss_benchXXX)

go build -o ${out_dir}/server $GOPATH/src/google.golang.org/grpc/benchmark/server/main.go && go build -o ${out_dir}/client $GOPATH/src/google.golang.org/grpc/benchmark/client/main.go
if [ $? != 0 ]; then
  exit 1
fi


while :
do
  run
  inc
done
