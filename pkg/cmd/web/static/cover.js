window.app = function () {
  return {
    speed: 1,
    asset: "covers",
    style: "",
    type: "",
    size: 100,
    error: "",
    page: 1,
    loading: false,
    images: [],
    nav: "home",
    pending: true,
    approved: false,
    rejected: false,
    background: false,
    nobackground: false,
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
      id = this.images[index].id;
      console.log(action + " " + id);
      this.error = "";
      let apiURL = "/api/" + this.asset + "/" + id + "/" + action;

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
          console.log("launch callback");
          callback(index);
        })
        .catch((error) => {
          this.error = error.message;
        });
    },
    likeImage: function (index) {
      this.action("like", index, () => {
        this.images[index].liked = true;
        this.images[index].state = 2;
      });
    },
    dislikeImage: function (index) {
      this.action("dislike", index, () => {
        this.images[index].liked = false;
      });
    },
    approveImage: function (index) {
      this.action("approve", index, () => {
        this.images[index].state = 2;
      });
    },
    rejectImage: function (index) {
      this.action("reject", index, () => {
        this.images[index].state = 1;
      });
    },
    search: function (page) {
      this.page = page;
      console.log("searching");
      this.error = "";
      this.loading = false;
      this.images = [];

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
      if (this.background !== this.nobackground) {
        if (this.background) {
          apiURL += "&background=true";
        } else {
          apiURL += "&background=false";
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
