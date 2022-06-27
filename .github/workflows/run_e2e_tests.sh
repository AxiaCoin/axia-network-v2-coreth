set -o errexit
set -o nounset
set -o pipefail

# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Testing specific variables
axia_testing_repo="avaplatform/axia-testing"
axia_repo="avaplatform/axia"
# Define default axia testing version to use
axia_testing_image="${axia_testing_repo}:master"

# Axia root directory
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$CORETH_PATH"/scripts/versions.sh

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Checks available docker tags exist
function docker_tag_exists() {
    TOKEN=$(curl -s -H "Content-Type: application/json" -X POST -d '{"username": "'${DOCKER_USERNAME}'", "password": "'${DOCKER_PASS}'"}' https://hub.docker.com/v2/users/login/ | jq -r .token)
    curl --silent -H "Authorization: JWT ${TOKEN}" -f --head -lL https://hub.docker.com/v2/repositories/$1/tags/$2/ > /dev/null
}

# Defines the axia-testing tag to use
# Either uses the same tag as the current branch or uses the default
if docker_tag_exists $axia_testing_repo $current_branch; then
    echo "$axia_testing_repo:$current_branch exists; using this image to run e2e tests"
    axia_testing_image="$axia_testing_repo:$current_branch"
else
    echo "$axia_testing_repo $current_branch does NOT exist; using the default image to run e2e tests"
fi

echo "Using $axia_testing_image for e2e tests"

# Defines the axia tag to use
# Either uses the same tag as the current branch or uses the default
# Disable matchup in favor of explicit tag
# TODO re-enable matchup when our workflow better supports it.
# if docker_tag_exists $axia_repo $current_branch; then
#     echo "$axia_repo:$current_branch exists; using this axia image to run e2e tests"
#     AXIA_VERSION=$current_branch
# else
#     echo "$axia_repo $current_branch does NOT exist; using the default image to run e2e tests"
# fi

# pulling the axia-testing image
docker pull $axia_testing_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current axia
source "$CORETH_PATH"/scripts/build_image.sh

# Target built version to use in axia-testing
axia_image="avaplatform/axia:$build_image_id"

echo "Running Axia Image: ${axia_image}"
echo "Running Axia Testing Image: ${axia_testing_image}"
echo "Git Commit ID : ${git_commit_id}"


# >>>>>>>> axia-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"axiaImage\":\"${axia_image}\",
    \"testBatch\":\"axia\"
}"
# >>>>>>>> axia-testing custom parameters <<<<<<<<<<<<<

bash "$CORETH_PATH/.kurtosis/kurtosis.sh" \
    --tests "AXC-Chain Bombard WorkFlow,Dynamic Fees,Snowman++ Correct Proposers and Timestamps" \
    --custom-params "${custom_params_json}" \
    "${axia_testing_image}" \
    $@
