window.app = function () {
    return {
      newsong: "",
      speed: 1,
      asset: "albums",
      style: "",
      type: "",
      size: 100,
      error: "",
      page: 1,
      loading: false,
      album: null,
      images: [],
      nav: "home",
      pending: true,
      approved: false,
      rejected: false,
      flagged: false,
      noflagged: false,
      ends: false,
      noends: false,
      liked: false,
      noliked: false,
      nav_home: function () {
        this.nav = "home";
        this.clear();
      },
      clear: function () {
        this.error = "";
        this.loading = false;
      },
      action: function (action, index, callback) {
        this.error = "";

        let apiURL = "/api/" + this.asset + "/" + this.album.id + "/" + action;
        if (index >= 0) {
          id = this.images[index].id;
          apiURL = "/api/" + this.asset + "/" + this.album.id + "/songs/"+ id + "/" + action;
        }
        this.fetch(apiURL, index, callback);
      },
      fetch: function(apiURL, index, callback) {
        fetch(apiURL, {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
          },
        })
          .then((response) => {
            if (response.ok) {
              return;
            } else {
              throw new Error(response.statusText);
            }
          })
          .then((data) => {
            console.log("launch callback")
            callback(index);
          })
          .catch((error) => {
            this.error = error.message;
          });
      },
      addSong: function () {
        id = this.newsong;
        if (id === "") {
          id = "-";
        }
        this.fetch("/api/albums/" + this.album.id + "/songs/"+id+"/add", 0, () => {
          this.search(this.page);
        });
      },
      deleteSong: function (index) {
        this.action("delete", index, () => {
          this.images.splice(index, 1);
        });
      },
      approveImage: function (index) {
        this.action("approve", index,  () => {
          this.album.state = 2;
        });
      },
      disapproveImage: function (index) {
        this.action("disapprove", index, () => {
          this.album.state = 0;
        });
      },
      deleteAlbum: function () {
        this.action("delete", -1, () => {
          this.search(this.page);
        });
      },
      changeSpeed() {
        if (this.speed === 3) {
          this.speed = 1;
        } else {
          this.speed++;
        }
        const audioElements = document.querySelectorAll("audio");
        // Iterate over the audio elements
        for (let i = 0; i < audioElements.length; i++) {
          audioElements[i].playbackRate = this.speed;
        }
      },
      play(index) {
        const audioElements = document.querySelectorAll("audio");
        const audioElement = audioElements[index];
        audioElement.playbackRate = this.speed;
        if (!audioElement.paused) {
          // Play the audio
          audioElement.play();
        }
      },
      search: function (page) {
        this.page = page;
        console.log("searching");
        this.error = "";
        this.loading = false;
        this.images = [];
        this.album = null;
  
        // URL encode the query string
        style = encodeURIComponent(this.style);
        type = encodeURIComponent(this.type);
  
        apiURL =
          "/api/" +
          this.asset +
          "?style=" +
          style +
          "&type=" +
          type +
          "&size=" +
          this.size +
          "&page=" +
          this.page;
  
        if (this.pending === true) {
          apiURL += "&pending=true";
        }
        if (this.approved === true) {
          apiURL += "&approved=true";
        }
        if (this.rejected === true) {
          apiURL += "&rejected=true";
        }
        if (this.flagged !== this.noflagged) {
          if (this.flagged) {
            apiURL += "&flagged=true";
          } else {
            apiURL += "&flagged=false";
          }
        }
        if (this.ends !== this.noends) {
          if (this.ends) {
            apiURL += "&ends=true";
          } else {
            apiURL += "&ends=false";
          }
        }
        if (this.liked !== this.noliked) {
          if (this.liked) {
            apiURL += "&liked=true";
          } else {
            apiURL += "&liked=false";
          }
        }
  
        this.loading = true;
        // Use fetch API to make a POST request to the API URL
        fetch(apiURL)
          .then((response) => {
            // Check if the response is ok (status code between 200 and 299)
            if (response.ok) {
              // Return the response as JSON
              return response.json();
            } else {
              // Throw an error with the status text
              throw new Error(response.statusText);
            }
          })
          .then((data) => {
            console.log(data);
            this.album = data;
            this.images = data.songs;
          })
          .catch((error) => {
            // Update the component's data properties with received error and empty summary
            this.error = error.message;
            this.images = [];
          })
          .finally(() => {
            this.loading = false;
          });
      },
    };
  };
  