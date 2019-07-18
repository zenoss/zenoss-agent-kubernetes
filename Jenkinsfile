node {
    def app

    stage('Clone repository') {
        checkout scm
    }

    stage('Build image') {
        app = docker.build("zenoss/zenoss-agent-kubernetes")
    }

    stage('Push image') {
        docker.withRegistry('https://registry.hub.docker.com', '37c42717-4227-4508-bf2e-bfd2152f5d5a') {
            app.push("${env.BUILD_NUMBER}")
            app.push("latest")
        }
    }
}
