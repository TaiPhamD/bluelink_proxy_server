sudo systemctl stop bluelink

# check if /opt/wolservice exists 
if [ ! -d "/opt/bluelink" ]; then
    # if not then create the directory
    sudo mkdir -p /opt/bluelink
else
    # remove old binary
    sudo rm /opt/bluelink/bluelink
fi

# install certbot and run
# sudo certbot certonly --standalone 
# if you want to setup HTTPS
# define https certs in the config.json file

# Copy binary to /opt/wolservice
sudo cp bluelinkgo_service /opt/bluelink/bluelink
sudo cp config_example.json /opt/bluelink/config.json # remember to edit this file
sudo cp bluelink.service /etc/systemd/system/
sudo systemctl enable bluelink
sudo systemctl start bluelink