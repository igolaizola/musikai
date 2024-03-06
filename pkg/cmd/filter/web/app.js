window.app = function () {
  return {
    asset: "songs",
    query: "",
    size: 100,
    error: "",
    page: 1,
    loading: false,
    images: [],
    nav: "home",
    approved: false,
    disapproved: false,
    disabled: false,
    enabled: false,
    flagged: false,
    noflagged: false,
    ends: false,
    noends: false,
    nav_home: function () {
      this.nav = "home";
      this.clear();
    },
    clear: function () {
      this.error = "";
      this.loading = false;
    },
    approveImage: function (index, value) {
      id = this.images[index].id;

      console.log("approving " + id);
      this.error = "";

      let apiURL = "/api/" + this.asset + "/" + id + "/approve";
      if (value === false) {
        apiURL = "/api/" + this.asset + "/" + id + "/disapprove";
      }

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
          this.images[index].approved = value;
        })
        .catch((error) => {
          this.error = error.message;
        });
    },
    disableImage: function (index, value) {
      id = this.images[index].id;

      console.log("disabling " + id);
      this.error = "";

      let apiURL = "/api/" + this.asset + "/" + id + "/disable";
      if (value === false) {
        apiURL = "/api/" + this.asset + "/" + id + "/enable";
      }

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
          if (value === true) {
            const audioElements = document.querySelectorAll("audio");
            const audioElement = audioElements[index];
            // Pause the audio if it's playing
            if (!audioElement.paused) {
              audioElement.pause();
              // Optional: Reset the audio time to 0
              audioElement.currentTime = 0;
            }
          }
          this.images[index].disabled = value;
        })
        .catch((error) => {
          this.error = error.message;
        });
    },
    search: function (page) {
      this.page = page;
      console.log("searching");
      this.error = "";
      this.loading = false;
      this.images = [];

      // URL encode the query string
      q = encodeURIComponent(this.query);

      apiURL =
        "/api/" +
        this.asset +
        "?query=" +
        q +
        "&size=" +
        this.size +
        "&page=" +
        this.page;

      if (this.approved !== this.disapproved) {
        if (this.approved) {
          apiURL += "&approved=true";
        } else {
          apiURL += "&approved=false";
        }
      }
      if (this.disabled !== this.enabled) {
        if (this.disabled) {
          apiURL += "&disabled=true";
        } else {
          apiURL += "&disabled=false";
        }
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
          this.images = data;
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
